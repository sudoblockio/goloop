package contract

import (
	"github.com/icon-project/goloop/common/log"
	"math/big"
	"reflect"
	"strings"

	"github.com/icon-project/goloop/service/scoreresult"

	"github.com/icon-project/goloop/common"
	"github.com/icon-project/goloop/common/codec"
	"github.com/icon-project/goloop/common/errors"
	"github.com/icon-project/goloop/module"
	"github.com/icon-project/goloop/service/scoreapi"
)

const (
	FUNC_PREFIX = "Ex_"
)

const (
	CID_CHAIN = "CID_CHAINSCORE"
)

var newSysScore = map[string]interface{}{
	CID_CHAIN: NewChainScore,
}

type SystemScore interface {
	Install(param []byte) error
	Update(param []byte) error
	GetAPI() *scoreapi.Info
}

func GetSystemScore(contentID string, params ...interface{}) (score SystemScore, err error) {
	defer func() {
		if e := recover(); e != nil {
			err = scoreresult.WithStatus(err, module.StatusSystemError)
		}
	}()
	v, ok := newSysScore[contentID]
	if ok == false {
		return nil, errors.InvalidStateError.Errorf("CID(%s)", contentID)
	}

	f := reflect.ValueOf(v)
	fType := f.Type()
	if len(params) != fType.NumIn() {
		return nil, errors.InvalidStateError.Errorf(
			"WrongParamNum(req:%d, pass:%d", fType.NumIn(), len(params))
	}

	in := make([]reflect.Value, len(params))
	for i, p := range params {
		pValue := reflect.ValueOf(p)
		if !pValue.IsValid() {
			in[i] = reflect.New(fType.In(i)).Elem()
			continue
		}
		if !pValue.Type().AssignableTo(fType.In(i)) {
			return nil,
				errors.InvalidStateError.Errorf(
					"Can't cast from %s to %s", pValue.Type(), fType.In(i))
		}
		in[i] = reflect.New(fType.In(i)).Elem()
		in[i].Set(pValue)
	}

	result := f.Call(in)

	if len(result) < 1 {
		return nil, errors.UnknownError.Errorf(
			"Fail to create system score.")
	}

	if result[0].IsNil() {
		return nil, errors.UnknownError.New(
			"Fail to create system score. Nil is returned.")
	}

	score, ok = result[0].Interface().(SystemScore)
	if ok == false {
		return nil, errors.UnknownError.Errorf(
			"Not SystemScore. Returned Type is %s", result[0].Type().String())
	}
	return score, nil
}

func CheckMethod(obj SystemScore) error {
	numMethod := reflect.ValueOf(obj).NumMethod()
	methodInfo := obj.GetAPI()
	invalid := false
	for i := 0; i < numMethod; i++ {
		m := reflect.TypeOf(obj).Method(i)
		if strings.HasPrefix(m.Name, FUNC_PREFIX) == false {
			continue
		}
		mName := strings.TrimPrefix(m.Name, FUNC_PREFIX)
		methodInfo := methodInfo.GetMethod(mName)
		if methodInfo == nil {
			continue
		}
		// CHECK INPUT
		numIn := m.Type.NumIn()
		if len(methodInfo.Inputs) != numIn-1 { //min receiver param
			return errors.InvalidStateError.Errorf("Wrong method input. method[%s]\n", mName)
		}
		var t reflect.Type
		for j := 1; j < numIn; j++ {
			t = m.Type.In(j)
			switch methodInfo.Inputs[j-1].Type {
			case scoreapi.Integer:
				if reflect.TypeOf(&common.HexInt{}) != t {
					invalid = true
				}
			case scoreapi.String:
				if reflect.TypeOf(string("")) != t {
					invalid = true
				}
			case scoreapi.Bytes:
				if reflect.TypeOf([]byte{}) != t {
					invalid = true
				}
			case scoreapi.Bool:
				if reflect.TypeOf(bool(false)) != t {
					invalid = true
				}
			case scoreapi.Address:
				if reflect.TypeOf(&common.Address{}).Implements(t) == false {
					invalid = true
				}
			default:
				invalid = true
			}
			if invalid == true {
				return errors.InvalidStateError.Errorf("wrong system score signature. method : %s, "+
					"expected input[%d] : %v BUT real type : %v", mName, j-1, methodInfo.Inputs[j-1].Type, t)
			}
		}

		numOut := m.Type.NumOut()
		if len(methodInfo.Outputs) != numOut-1 { // minus error
			return errors.InvalidStateError.Errorf("Wrong method output. method[%s]\n", mName)
		}
		for j := 0; j < len(methodInfo.Outputs); j++ {
			t := m.Type.Out(j)
			switch methodInfo.Outputs[j] {
			case scoreapi.Integer:
				if reflect.TypeOf(int(0)) != t && reflect.TypeOf(int64(0)) != t {
					invalid = true
				}
			case scoreapi.String:
				if reflect.TypeOf(string("")) != t {
					invalid = true
				}
			case scoreapi.Bytes:
				if reflect.TypeOf([]byte{}) != t {
					invalid = true
				}
			case scoreapi.Bool:
				if reflect.TypeOf(bool(false)) != t {
					invalid = true
				}
			case scoreapi.Address:
				if reflect.TypeOf(&common.Address{}).Implements(t) == false {
					invalid = true
				}
			case scoreapi.List:
				if t.Kind() != reflect.Slice && t.Kind() != reflect.Array {
					invalid = true
				}
			case scoreapi.Dict:
				if t.Kind() != reflect.Map {
					invalid = true
				}
			default:
				invalid = true
			}
			if invalid == true {
				return errors.InvalidStateError.Errorf("Wrong system score signature. method : %s, "+
					"expected output[%d] : %v BUT real type : %v", mName, j, methodInfo.Outputs[j], t)
			}
		}
	}
	return nil
}

func Invoke(score SystemScore, method string, paramObj *codec.TypedObj) (status module.Status, result *codec.TypedObj, steps *big.Int) {
	defer func() {
		if err := recover(); err != nil {
			log.Debugf("Fail to sysCall method[%s]. err=%+v\n", method, err)
			status = module.StatusSystemError
		}
	}()
	steps = big.NewInt(0)
	m := reflect.ValueOf(score).MethodByName(FUNC_PREFIX + method)
	if m.IsValid() == false {
		return module.StatusMethodNotFound, nil, steps
	}
	mType := m.Type()

	var params []interface{}
	if ps, err := common.DecodeAny(paramObj); err != nil {
		return module.StatusInvalidParameter, nil, steps
	} else {
		var ok bool
		params, ok = ps.([]interface{})
		if !ok {
			return module.StatusInvalidParameter, nil, steps
		}
	}

	if len(params) != mType.NumIn() {
		return module.StatusInvalidParameter, nil, steps
	}

	objects := make([]reflect.Value, len(params))
	for i, p := range params {
		oType := mType.In(i)
		pValue := reflect.ValueOf(p)
		if !pValue.IsValid() {
			objects[i] = reflect.New(mType.In(i)).Elem()
			continue
		}
		if !pValue.Type().AssignableTo(oType) {
			return module.StatusInvalidParameter, nil, steps
		}
		objects[i] = reflect.New(mType.In(i)).Elem()
		objects[i].Set(pValue)
	}

	// check if it is eventLog or not.
	// if eventLog then cc.AddLog().
	r := m.Call(objects)
	resultLen := len(r)
	var output interface{}

	// last output type in chain score method is error.
	status = module.StatusSuccess
	for i, v := range r {
		if i+1 == resultLen { // last output
			if err := v.Interface(); err != nil {
				if e, ok := err.(error); ok {
					log.Debugf("Method %s returns failure err=%v\n", method, e)
					status, _ = scoreresult.StatusOf(e)
				} else {
					status = module.StatusSystemError
				}
			}
			continue
		} else {
			output = v.Interface()
		}
	}

	result, _ = common.EncodeAny(output)
	// TODO apply used step
	return status, result, steps
}
