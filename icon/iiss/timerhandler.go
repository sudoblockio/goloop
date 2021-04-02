/*
 * Copyright 2020 ICON Foundation
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
package iiss

import (
	"github.com/icon-project/goloop/common"
	"github.com/icon-project/goloop/service/state"
	"math/big"
)

func (s *ExtensionStateImpl) HandleTimerJob(wc state.WorldContext) (err error) {
	bt := s.GetUnbondingTimerState(wc.BlockHeight(), false)
	if bt != nil {
		err = s.handleUnbondingTimer(bt.Addresses, bt.Height)
		if err != nil {
			return
		}
	}

	st := s.GetUnstakingTimerState(wc.BlockHeight(), false)
	if st != nil {
		err = s.handleUnstakingTimer(wc, st.Addresses, st.Height)
	}
	return
}

func (s *ExtensionStateImpl) handleUnstakingTimer(wc state.WorldContext, al []*common.Address, h int64) error {
	for _, a := range al {
		ea := s.GetAccount(a)
		ra, err := ea.RemoveUnstaking(h)
		if err != nil {
			return err
		}

		wa := wc.GetAccountState(ea.Address().ID())
		b := wa.GetBalance()
		wa.SetBalance(new(big.Int).Add(b, ra))
	}
	return nil
}

func (s *ExtensionStateImpl) handleUnbondingTimer(al []*common.Address, h int64) error {
	for _, a := range al {
		as := s.GetAccount(a)
		if err := as.RemoveUnbonding(h); err != nil {
			return err
		}
	}
	return nil
}
