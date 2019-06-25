package network

import (
	"context"
	"fmt"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/icon-project/goloop/common/codec"
	"github.com/icon-project/goloop/common/db"
	"github.com/icon-project/goloop/module"
)

const (
	testNumValidator      = 4
	testNumSeed           = 4
	testNumCitizen        = 4
	testNumAllowedPeer    = 8
	testNumNotAllowedPeer = 2
	testProtoPriority     = 1
)

var (
	ProtoTestNetworkBroadcast module.ProtocolInfo = protocolInfo(0x0100)
	ProtoTestNetworkMulticast module.ProtocolInfo = protocolInfo(0x0200)
	ProtoTestNetworkRequest   module.ProtocolInfo = protocolInfo(0x0300)
	ProtoTestNetworkResponse  module.ProtocolInfo = protocolInfo(0x0400)
	ProtoTestNetworkNeighbor  module.ProtocolInfo = protocolInfo(0x0500)
)

var (
	testSubProtocols = []module.ProtocolInfo{
		ProtoTestNetworkBroadcast,
		ProtoTestNetworkMulticast,
		ProtoTestNetworkRequest,
		ProtoTestNetworkResponse,
		ProtoTestNetworkNeighbor,
	}
)

type testReactor struct {
	name         string
	ph           module.ProtocolHandler
	log          *logger
	t            *testing.T
	nm           module.NetworkManager
	nt           module.NetworkTransport
	p2p          *PeerToPeer
	ch           chan<- context.Context
	responseFunc func(r *testReactor, rm *testNetworkRequest, id module.PeerID) error
}

func newTestReactor(name string, nm module.NetworkManager, t *testing.T) *testReactor {
	r := &testReactor{name: name, nm: nm, log: newLogger("TestReactor", name), t: t}
	ph, err := nm.RegisterReactor(name, r, testSubProtocols, testProtoPriority)
	assert.NoError(t, err, "RegisterReactor")
	r.ph = ph
	r.p2p = nm.(*manager).p2p
	r.p2p.setEventCbFunc(p2pEventNotAllowed, r.ph.(*protocolHandler).protocol.Uint16(), r.onEvent)
	r.t.Log(time.Now(), r.name, "newTestReactor", r.p2p.self.id)
	return r
}

type testNetworkMessage struct {
	Message string
}

type testNetworkBroadcast struct {
	Message string
}

type testNetworkMulticast struct {
	Message string
}

type testNetworkRequest struct {
	Message string
}

type testNetworkResponse struct {
	Message string
}

func (r *testReactor) OnReceive(pi module.ProtocolInfo, b []byte, id module.PeerID) (re bool, err error) {
	r.log.Println("OnReceive", pi, b, id)
	var msg string
	switch pi {
	case ProtoTestNetworkBroadcast:
		rm := &testNetworkBroadcast{}
		r.decode(b, rm)
		msg = rm.Message
		r.log.Println("handleProtoTestNetworkBroadcast", rm, id)
		r.t.Log(time.Now(), r.name, "OnReceive", rm, r.p2pConn())
		re = true
	case ProtoTestNetworkNeighbor:
		rm := &testNetworkBroadcast{}
		r.decode(b, rm)
		msg = rm.Message
		r.log.Println("handleProtoTestNetworkNeighbor", rm, id)
		r.t.Log(time.Now(), r.name, "OnReceive", rm, r.p2pConn())
		re = false
	case ProtoTestNetworkMulticast:
		rm := &testNetworkMulticast{}
		r.decode(b, rm)
		msg = rm.Message
		r.log.Println("handleProtoTestNetworkMulticast", rm, id)
		r.t.Log(time.Now(), r.name, "OnReceive", rm, r.p2pConn())
		re = true
	case ProtoTestNetworkRequest:
		rm := &testNetworkRequest{}
		r.decode(b, rm)
		msg = rm.Message
		r.log.Println("handleProtoTestNetworkRequest", rm, id)
		r.t.Log(time.Now(), r.name, "OnReceive", rm)
		if r.responseFunc != nil {
			err = r.responseFunc(r, rm, id)
		} else {
			r.Response(rm.Message, id)
		}
	case ProtoTestNetworkResponse:
		rm := &testNetworkResponse{}
		r.decode(b, rm)
		msg = rm.Message
		r.log.Println("handleProtoTestNetworkResponse", rm, id)
		r.t.Log(time.Now(), r.name, "OnReceive", rm)
	default:
		re = false
	}
	ctx := context.WithValue(context.Background(), "op", "recv")
	ctx = context.WithValue(ctx, "pi", pi)
	ctx = context.WithValue(ctx, "msg", msg)
	ctx = context.WithValue(ctx, "name", r.name)
	r.ch <- ctx
	return
}

func (r *testReactor) OnFailure(err error, pi module.ProtocolInfo, b []byte) {
	rm := &testNetworkMessage{}
	r.decode(b, rm)
	msg := rm.Message
	ctx := context.WithValue(context.Background(), "op", "error")
	ctx = context.WithValue(ctx, "pi", pi)
	ctx = context.WithValue(ctx, "msg", msg)
	ctx = context.WithValue(ctx, "name", r.name)
	ctx = context.WithValue(ctx, "error", err)
	r.ch <- ctx
}
func (r *testReactor) OnJoin(id module.PeerID) {
	r.log.Println("OnJoin", id)
	ctx := context.WithValue(context.Background(), "op", "join")
	ctx = context.WithValue(ctx, "p2pConnInfo", newP2PConnInfo(r.p2p))
	ctx = context.WithValue(ctx, "name", r.name)
	r.ch <- ctx
}
func (r *testReactor) OnLeave(id module.PeerID) {
	r.log.Println("OnLeave", id)
}
func (r *testReactor) onEvent(evt string, p *Peer) {
	r.log.Println("onEvent", evt, p.id)
	ctx := context.WithValue(context.Background(), "op", "event")
	ctx = context.WithValue(ctx, "event", evt)
	ctx = context.WithValue(ctx, "name", r.name)
	ctx = context.WithValue(ctx, "peer", p.id)
	r.ch <- ctx
}

func (r *testReactor) encode(v interface{}) []byte {
	b := make([]byte, DefaultPacketBufferSize)
	enc := codec.MP.NewEncoderBytes(&b)
	if err := enc.Encode(v); err != nil {
		log.Panicf("Fail to encode err=%+v", err)
	}
	return b
}

func (r *testReactor) decode(b []byte, v interface{}) {
	codec.MP.MustUnmarshalFromBytes(b, v)
}

func (r *testReactor) p2pConn() string {
	return newP2PConnInfo(r.p2p).String()
}

type p2pConnInfo struct {
	role     PeerRoleFlag
	friends  int
	parent   int
	uncles   int
	children int
	nephews  int
}

func newP2PConnInfo(p2p *PeerToPeer) *p2pConnInfo { //p2p.connections()
	parent := 0
	if p2p.parent != nil {
		parent = 1
	}
	return &p2pConnInfo{p2p.getRole(), p2p.friends.Len(), parent, p2p.uncles.Len(), p2p.children.Len(), p2p.nephews.Len()}
}
func (ci *p2pConnInfo) String() string {
	return fmt.Sprintf("role:%d, friends:%d, parent:%d, uncle:%d, children:%d, nephew:%d",
		ci.role,
		ci.friends,
		ci.parent,
		ci.uncles,
		ci.children,
		ci.nephews)
}

func (r *testReactor) Broadcast(msg string) string {
	m := &testNetworkBroadcast{Message: fmt.Sprintf("Broadcast.%s.%s", msg, r.name)}
	r.t.Log(time.Now(), r.name, "Broadcast", m, r.p2pConn())
	err := r.ph.Broadcast(ProtoTestNetworkBroadcast, r.encode(m), module.BROADCAST_ALL)
	assert.NoError(r.t, err, m.Message)
	r.log.Println("Broadcast", m)
	return m.Message
}

func (r *testReactor) BroadcastNeighbor(msg string) string {
	m := &testNetworkBroadcast{Message: fmt.Sprintf("BroadcastNeighbor.%s.%s", msg, r.name)}
	r.t.Log(time.Now(), r.name, "BroadcastNeighbor", m, r.p2pConn())
	err := r.ph.Broadcast(ProtoTestNetworkNeighbor, r.encode(m), module.BROADCAST_NEIGHBOR)
	assert.NoError(r.t, err, m.Message)
	r.log.Println("BroadcastNeighbor", m)
	return m.Message
}

func (r *testReactor) Multicast(msg string) string {
	m := &testNetworkMulticast{Message: fmt.Sprintf("Multicast.%s.%s", msg, r.name)}
	r.t.Log(time.Now(), r.name, "Multicast", m, r.p2pConn())
	err := r.ph.Multicast(ProtoTestNetworkMulticast, r.encode(m), module.ROLE_VALIDATOR)
	assert.NoError(r.t, err, m.Message)
	r.log.Println("Multicast", m)
	return m.Message
}

func (r *testReactor) Request(msg string, id module.PeerID) string {
	m := &testNetworkRequest{Message: fmt.Sprintf("Request.%s.%s", msg, r.name)}
	r.t.Log(time.Now(), r.name, "Request", m, r.p2pConn())
	err := r.ph.Unicast(ProtoTestNetworkRequest, r.encode(m), id)
	assert.NoError(r.t, err, m.Message)
	r.log.Println("Request", m, id)
	return m.Message
}

func (r *testReactor) Response(msg string, id module.PeerID) string {
	m := &testNetworkResponse{Message: fmt.Sprintf("Response.%s.%s", msg, r.name)}
	r.t.Log(time.Now(), r.name, "Response", m, r.p2pConn())
	err := r.ph.Unicast(ProtoTestNetworkResponse, r.encode(m), id)
	assert.NoError(r.t, err, m.Message)
	r.log.Println("Response", m, id)
	return m.Message
}

type dummyChain struct {
	nid       int
	metricCtx context.Context
}

func (c *dummyChain) Database() db.Database                             { panic("not implemented") }
func (c *dummyChain) Wallet() module.Wallet                             { panic("not implemented") }
func (c *dummyChain) NID() int                                          { return c.nid }
func (c *dummyChain) Channel() string                                   { panic("not implemented") }
func (c *dummyChain) ConcurrencyLevel() int                             { panic("not implemented") }
func (c *dummyChain) NormalTxPoolSize() int                             { panic("not implemented") }
func (c *dummyChain) PatchTxPoolSize() int                              { panic("not implemented") }
func (c *dummyChain) MaxBlockTxBytes() int                              { panic("not implemented") }
func (c *dummyChain) Genesis() []byte                                   { panic("not implemented") }
func (c *dummyChain) GetGenesisData(key []byte) ([]byte, error)         { panic("not implemented") }
func (c *dummyChain) CommitVoteSetDecoder() module.CommitVoteSetDecoder { panic("not implemented") }

func (c *dummyChain) BlockManager() module.BlockManager     { panic("not implemented") }
func (c *dummyChain) Consensus() module.Consensus           { panic("not implemented") }
func (c *dummyChain) ServiceManager() module.ServiceManager { panic("not implemented") }
func (c *dummyChain) NetworkManager() module.NetworkManager { panic("not implemented") }
func (c *dummyChain) Regulator() module.Regulator           { panic("not implemented") }

func (c *dummyChain) Init(sync bool) error  { panic("not implemented") }
func (c *dummyChain) Start(sync bool) error { panic("not implemented") }
func (c *dummyChain) Stop(sync bool) error  { panic("not implemented") }
func (c *dummyChain) Term(sync bool) error  { panic("not implemented") }
func (c *dummyChain) State() string         { panic("not implemented") }

func (c *dummyChain) Reset(sync bool) error  { panic("not implemented") }
func (c *dummyChain) Verify(sync bool) error { panic("not implemented") }

func (c *dummyChain) MetricContext() context.Context { return c.metricCtx }

func generateNetwork(name string, port int, n int, t *testing.T, roles ...module.Role) ([]*testReactor, int) {
	arr := make([]*testReactor, n)
	for i := 0; i < n; i++ {
		w := walletFromGeneratedPrivateKey()
		nt := NewTransport(fmt.Sprintf("127.0.0.1:%d", port+i), w)
		c := &dummyChain{nid: i, metricCtx: context.Background()}
		nm := NewManager(c, nt, "", roles...)
		r := newTestReactor(fmt.Sprintf("%s_%d", name, i), nm, t)
		r.nt = nt
		if err := r.nt.Listen(); err != nil {
			t.Fatal(err)
		}
		if err := nm.Start(); err != nil {
			t.Fatal(err)
		}
		arr[i] = r
	}
	return arr, port + n
}

func timeout(ch <-chan string, d time.Duration) (string, error) {
	t := time.NewTimer(d)
	select {
	case s := <-ch:
		return s, nil
	case <-t.C:
		return "", fmt.Errorf("timeout d:%v", d)
	}
}

func timeoutCtx(ch <-chan context.Context, d time.Duration, k interface{}) (context.Context, error) {
	t := time.NewTimer(d)
	for {
		select {
		case s := <-ch:
			if s.Value(k) != nil {
				return s, nil
			}
			str := ""
			for _, key := range []string{"op", "pi", "msg", "name", "p2pConnInfo", "event", "peer", "error"} {
				str += fmt.Sprintf("%s:%#v,", key, s.Value(key))
			}
			log.Println("ignore timeoutCtx", str)
		case <-t.C:
			return nil, fmt.Errorf("timeout d:%v", d)
		}
	}
}

func wait(ch <-chan context.Context, pi module.ProtocolInfo, msg string, n int, d time.Duration, dest ...string) error {
	rn := 0
	t := time.NewTimer(d)
	m := make(map[string]int)
	for _, rname := range dest {
		m[rname] = 0
	}
	for {
		select {
		case ctx := <-ch:
			trpi := ctx.Value("pi")
			if trpi == nil {
				continue
			}
			rpi := trpi.(module.ProtocolInfo)
			rmsg := ctx.Value("msg").(string)
			rname := ctx.Value("name").(string)
			if rpi.Uint16() == pi.Uint16() && msg == rmsg {
				z := len(m)
				if z > 0 {
					if c, ok := m[rname]; ok {
						m[rname] = c + 1
					}
					for _, c := range m {
						if c > 0 {
							z--
						}
					}
					if z < 1 {
						return nil
					}
				} else {
					rn++
					if rn >= n {
						return nil
					}
				}
			}
		case <-t.C:
			return fmt.Errorf("timeout d:%v pi:%x msg:%s n:%d rn:%d dest:%v", d, pi.Uint16(), msg, n, rn, dest)
		}
	}
}
func waitConnection(ch <-chan context.Context, limit []int, n int, d time.Duration) (map[string]time.Duration, time.Duration, error) {
	t := time.NewTimer(d)
	m := make(map[string]time.Duration)
	s := time.Now()
	var maxD time.Duration
	for {
		select {
		case ctx := <-ch:
			tci := ctx.Value("p2pConnInfo")
			if tci == nil {
				continue
			}
			ci := tci.(*p2pConnInfo)
			rname := ctx.Value("name").(string)
			switch ci.role {
			case p2pRoleRoot, p2pRoleRootSeed:
				if ci.friends == limit[p2pConnTypeFriend] &&
					ci.children == limit[p2pConnTypeChildren] && ci.nephews == limit[p2pConnTypeNephew] {
					if _, ok := m[rname]; !ok {
						m[rname] = time.Since(s)
					}
				}
			case p2pRoleSeed:
				if ci.parent == limit[p2pConnTypeParent] && ci.uncles == limit[p2pConnTypeUncle] &&
					ci.children == limit[p2pConnTypeChildren] && ci.nephews == limit[p2pConnTypeNephew] {
					if _, ok := m[rname]; !ok {
						m[rname] = time.Since(s)
					}
				}
			case p2pRoleNone:
				if ci.parent == limit[p2pConnTypeParent] && ci.uncles == limit[p2pConnTypeUncle] {
					if _, ok := m[rname]; !ok {
						m[rname] = time.Since(s)
					}
				}
			}
			if len(m) >= n {
				for _, md := range m {
					if maxD < md {
						maxD = md
					}
				}
				return m, maxD, nil
			}
		case <-t.C:
			for _, md := range m {
				if maxD < md {
					maxD = md
				}
			}
			return m, maxD, fmt.Errorf("timeout d:%v, limit:%v, n:%d, rn:%d", d, limit, n, len(m))
		}
	}
}

func waitEvent(ch <-chan context.Context, n int, d time.Duration, evt string, peers ...module.PeerID) (map[string]map[string]int, error) {
	t := time.NewTimer(d)
	m := make(map[string]map[string]int)
	for _, p := range peers {
		m[p.String()] = make(map[string]int)
	}
	for {
		select {
		case ctx := <-ch:
			tevt := ctx.Value("event")
			if tevt == nil {
				continue
			}
			revt := tevt.(string)
			rpeer := ctx.Value("peer").(module.PeerID)
			rname := ctx.Value("name").(string)
			if revt == evt {
				rm := m[rpeer.String()]
				if _, ok := rm[rname]; !ok {
					rm[rname] = 0
				}
				rm[rname]++

				done := true
				for _, tm := range m {
					if len(tm) < n {
						done = false
						break
					}
				}
				if done {
					return m, nil
				}

			}
		case <-t.C:
			return m, fmt.Errorf("timeout d:%v, evt:%v, peers:%v, n:%d, rn:%d", d, evt, peers, n, len(m))
		}
	}
}

func dailByMap(t *testing.T, m map[string][]*testReactor, na NetAddress, delay time.Duration) {
	for _, arr := range m {
		dailByList(t, arr, na, delay)
	}
}
func dailByList(t *testing.T, arr []*testReactor, na NetAddress, delay time.Duration) {
	for _, r := range arr {
		if r.p2p.self.netAddress != na {
			err := r.p2p.dial(na)
			assert.NoError(t, err, "dial", r.name, "->", na)
			if delay > 0 {
				time.Sleep(delay)
			}
		}
	}
}

func listenerClose(t *testing.T, m map[string][]*testReactor) {
	for _, arr := range m {
		for _, r := range arr {
			log.Println("Try stopping", r.name)
			r.nm.Term()
			assert.NoError(t, r.nt.Close(), "Close", r.name)
		}
	}
}

func Test_network_basic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}
	log.SetFlags(log.Lmicroseconds)
	m := make(map[string][]*testReactor)
	p := 8080
	m["TestCitizen"], p = generateNetwork("TestCitizen", p, testNumCitizen, t)                              //8080~8083
	m["TestSeed"], p = generateNetwork("TestSeed", p, testNumSeed, t, module.ROLE_SEED)                     //8084~8087
	m["TestValidator"], p = generateNetwork("TestValidator", p, testNumValidator, t, module.ROLE_VALIDATOR) //8088~8091

	sr := m["TestSeed"][0]
	dailByMap(t, m, sr.p2p.self.netAddress, 100*time.Millisecond)

	ch := make(chan context.Context, testNumCitizen+testNumSeed+testNumValidator)
	for _, v := range m {
		for _, r := range v {
			r.ch = ch
		}
	}

	limit := []int{0, 1, DefaultChildrenLimit, DefaultUncleLimit, DefaultNephewLimit, testNumValidator - 1}
	n := testNumValidator + testNumSeed + testNumCitizen
	connMap, maxD, err := waitConnection(ch, limit, n, 10*DefaultSeedPeriod)
	t.Log(time.Now(), "max:", maxD, connMap)
	assert.NoError(t, err, "waitConnection", connMap)

	t.Log(time.Now(), "Messaging")

	msg := m["TestValidator"][0].Broadcast("Test1")
	n = testNumValidator - 1 + testNumSeed + testNumCitizen
	err = wait(ch, ProtoTestNetworkBroadcast, msg, n, time.Second)
	assert.NoError(t, err, "Broadcast", "Test1")

	msg = m["TestValidator"][0].BroadcastNeighbor("Test2")
	n = testNumValidator - 1 + DefaultChildrenLimit + DefaultNephewLimit
	err = wait(ch, ProtoTestNetworkNeighbor, msg, n, time.Second)
	assert.NoError(t, err, "BroadcastNeighbor", "Test2")

	msg = m["TestValidator"][0].Multicast("Test3")
	n = testNumValidator - 1
	err = wait(ch, ProtoTestNetworkMulticast, msg, n, time.Second)
	assert.NoError(t, err, "Multicast", "Test3")

	msg = m["TestSeed"][0].Multicast("Test4")
	n = testNumValidator
	err = wait(ch, ProtoTestNetworkMulticast, msg, n, time.Second)
	assert.NoError(t, err, "Multicast", "Test4")

	msg = m["TestCitizen"][0].Multicast("Test5")
	n = testNumValidator + 1 + DefaultUncleLimit
	err = wait(ch, ProtoTestNetworkMulticast, msg, n, time.Second+DefaultAlternateSendPeriod)
	assert.NoError(t, err, "Multicast", "Test5")

	tr := sr
	for _, r := range m["TestSeed"] {
		p := m["TestCitizen"][0].p2p.getPeer(r.nt.PeerID(), true)
		if p != nil {
			tr = r
			break
		}
	}
	respCh := make(chan string, 1)
	tr.responseFunc = func(r *testReactor, rm *testNetworkRequest, id module.PeerID) error {
		m := r.Response(rm.Message, id)
		respCh <- m
		return nil
	}
	msg = m["TestCitizen"][0].Request("Test6", tr.nt.PeerID())
	err = wait(ch, ProtoTestNetworkRequest, msg, 1, time.Second, tr.name)
	assert.NoError(t, err, "Request", "Test6")

	msg, err = timeout(respCh, time.Second)
	assert.NoError(t, err, "timeout", "responseFunc")

	err = wait(ch, ProtoTestNetworkResponse, msg, 1, time.Second, m["TestCitizen"][0].name)
	assert.NoError(t, err, "Response", "Test6")

	time.Sleep(2 * DefaultAlternateSendPeriod)

	listenerClose(t, m)
	t.Log(time.Now(), "Finish")
}

func Test_network_allowedPeer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}
	m := make(map[string][]*testReactor)
	p := 8080
	m["TestAllowed"], p = generateNetwork("TestAllowed", p, testNumAllowedPeer, t, module.ROLE_VALIDATOR, module.ROLE_VALIDATOR)
	m["TestNotAllowed"], p = generateNetwork("TestNotAllowed", p, testNumNotAllowedPeer, t, module.ROLE_VALIDATOR, module.ROLE_VALIDATOR)
	allowed := make([]module.PeerID, 0)
	notAllowed := make([]module.PeerID, 0)

	for _, r := range m["TestAllowed"] {
		allowed = append(allowed, r.nt.PeerID())
	}
	for _, r := range m["TestNotAllowed"] {
		notAllowed = append(notAllowed, r.nt.PeerID())
	}
	for _, r := range m["TestAllowed"] {
		r.nm.SetRole(0, module.ROLE_NORMAL, allowed...)
	}
	sr := m["TestAllowed"][0]
	dailByMap(t, m, sr.p2p.self.netAddress, 100*time.Millisecond)

	ch := make(chan context.Context, testNumAllowedPeer+testNumNotAllowedPeer)
	for _, v := range m {
		for _, r := range v {
			r.ch = ch
		}
	}

	limit := []int{0, 0, 0, 0, 0, testNumAllowedPeer - 1}
	n := testNumAllowedPeer
	connMap, maxD, err := waitConnection(ch, limit, n, 10*DefaultSeedPeriod)
	t.Log(time.Now(), "max:", maxD, connMap)
	assert.NoError(t, err, "waitConnection", connMap)

	go func() {
		for _, r := range m["TestAllowed"] {
			dailByList(t, m["TestNotAllowed"], r.p2p.self.netAddress, 0)
		}
	}()
	evtMap, err := waitEvent(ch, n, 2*time.Second, p2pEventNotAllowed, notAllowed...)
	t.Log(time.Now(), "Before", evtMap)
	assert.NoError(t, err, "waitEvent", evtMap)

	t.Log(time.Now(), "Messaging")
	msg := m["TestAllowed"][0].Broadcast("Test1")
	n = testNumAllowedPeer - 1
	err = wait(ch, ProtoTestNetworkBroadcast, msg, n, time.Second)
	assert.NoError(t, err, "Broadcast", "Test1")

	remove := allowed[testNumAllowedPeer-1]
	go func() {
		for _, r := range m["TestAllowed"] {
			r.nm.RemoveRole(module.ROLE_NORMAL, remove)
		}
	}()
	evtMap, err = waitEvent(ch, n-1, 2*time.Second, p2pEventNotAllowed, remove)
	t.Log(time.Now(), "After", evtMap)
	assert.NoError(t, err, "waitEvent2", evtMap)

	msg = m["TestAllowed"][0].Broadcast("Test2")
	n = testNumAllowedPeer - 2
	err = wait(ch, ProtoTestNetworkBroadcast, msg, n, time.Second)
	assert.NoError(t, err, "Broadcast", "Test2")

	listenerClose(t, m)
	t.Log(time.Now(), "Finish")
}

var (
	zeroQueue = &queue{
		buf:  make([]context.Context, 1),
		w:    0,
		r:    0,
		size: 0,
		len:  1,
		wait: make(map[chan bool]interface{}),
	}
)

type testQueue struct {
	Queue
	ch  chan bool
	mtx sync.Mutex
}

func newTestQueue(size int) *testQueue {
	q := &queue{
		buf:  make([]context.Context, size+1),
		w:    0,
		r:    0,
		size: size,
		len:  size + 1,
		wait: make(map[chan bool]interface{}),
	}
	return &testQueue{Queue: q}
}

func (q *testQueue) Pop() context.Context {
	if q.ch != nil {
		<-q.ch
	}
	return q.Queue.Pop()
}
func (q *testQueue) pending() {
	defer q.mtx.Unlock()
	q.mtx.Lock()
	if q.ch == nil {
		q.ch = make(chan bool)
	}
}
func (q *testQueue) resume() {
	defer q.mtx.Unlock()
	q.mtx.Lock()
	if q.ch != nil {
		close(q.ch)
		q.ch = nil
	}
}

func Test_network_failure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}
	m := make(map[string][]*testReactor)
	p := 8080
	m["TestCitizen"], p = generateNetwork("TestCitizen", p, testNumCitizen, t)                              //8080~8083
	m["TestSeed"], p = generateNetwork("TestSeed", p, testNumSeed, t, module.ROLE_SEED)                     //8084~8087
	m["TestValidator"], p = generateNetwork("TestValidator", p, testNumValidator, t, module.ROLE_VALIDATOR) //8088~8091

	sr := m["TestSeed"][0]
	dailByMap(t, m, sr.p2p.self.netAddress, 100*time.Millisecond)

	ch := make(chan context.Context, testNumCitizen+testNumSeed+testNumValidator)
	for _, v := range m {
		for _, r := range v {
			r.ch = ch
		}
	}

	limit := []int{0, 1, DefaultChildrenLimit, DefaultUncleLimit, DefaultNephewLimit, testNumValidator - 1}
	n := testNumValidator + testNumSeed + testNumCitizen
	connMap, maxD, err := waitConnection(ch, limit, n, 10*DefaultSeedPeriod)
	t.Log(time.Now(), "max:", maxD, connMap)
	assert.NoError(t, err, "waitConnection", connMap)

	t.Log(time.Now(), "Messaging")

	msg := m["TestValidator"][0].Broadcast("Test1")
	n = testNumValidator - 1 + testNumSeed + testNumCitizen
	err = wait(ch, ProtoTestNetworkBroadcast, msg, n, time.Second)
	assert.NoError(t, err, "Broadcast", "Test1")

	qm := make(map[string]Queue)
	pArr := make([]*Peer, 0)
	pArr = append(pArr, m["TestValidator"][0].p2p.friends.Array()...)
	pArr = append(pArr, m["TestValidator"][0].p2p.children.Array()...)
	pArr = append(pArr, m["TestValidator"][0].p2p.nephews.Array()...)
	//peer.send ErrQueueOverflow
	for _, p := range pArr {
		qm[p.id.String()] = p.q.s[testProtoPriority]
		p.q.s[testProtoPriority] = zeroQueue
	}

	msg = m["TestValidator"][0].Broadcast("Test2")
	ctx, err := timeoutCtx(ch, 2*DefaultAlternateSendPeriod, "error")
	assert.NoError(t, err, "Broadcast", "Test2")
	if ctx != nil {
		err = ctx.Value("error").(error)
		rname := ctx.Value("name").(string)
		assert.Equal(t, m["TestValidator"][0].name, rname, "")
		assert.EqualError(t, ErrQueueOverflow, err.Error(), "")
	}

	//peer.send ErrNotAvailable
	for _, p := range pArr {
		tq := newTestQueue(DefaultPeerSendQueueSize)
		tq.pending()
		p.q.s[testProtoPriority] = tq
	}

	msg = m["TestValidator"][0].Broadcast("Test3")
	go func() {
		for i, p := range pArr {
			log.Println("Close by testErrNotAvailable", i, len(pArr), p.id)
			tq := p.q.s[testProtoPriority].(*testQueue)
			go func(tq *testQueue) {
				tq.resume()
			}(tq)
			p.Close("testErrNotAvailable")
		}
	}()

	//go func() {
	//	for _, p := range pArr {
	//		tq := p.q.s[testProtoPriority].(*testQueue)
	//		tq.resume()
	//	}
	//}()

	ctx, err = timeoutCtx(ch, 5*DefaultAlternateSendPeriod, "error")
	assert.NoError(t, err, "Broadcast", "Test3")
	if ctx != nil {
		errStr := ""
		if terr, ok := ctx.Value("error").(error); ok {
			errStr = terr.Error()
		}
		rname := ctx.Value("name").(string)
		assert.Equal(t, m["TestValidator"][0].name, rname, "")
		assert.EqualError(t, ErrNotAvailable, errStr, "")
	}

	for _, p := range pArr {
		p.q.s[testProtoPriority] = qm[p.id.String()]
	}

	//msg = m["TestValidator"][0].Broadcast("Test4")
	//n = testNumValidator - 1 + testNumSeed + testNumCitizen
	//err = wait(ch, ProtoTestNetworkBroadcast, msg, n, time.Second)
	//assert.NoError(t, err, "Broadcast", "Test4")

	time.Sleep(5 * time.Second)

	listenerClose(t, m)
	t.Log(time.Now(), "Finish")
}
