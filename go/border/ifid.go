// Copyright 2016 ETH Zurich
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"time"

	log "github.com/inconshreveable/log15"

	"github.com/netsec-ethz/scion/go/border/packet"
	"github.com/netsec-ethz/scion/go/border/path"
	"github.com/netsec-ethz/scion/go/lib/addr"
	"github.com/netsec-ethz/scion/go/lib/log"
	"github.com/netsec-ethz/scion/go/proto"
)

const IFIDFreq = 1 * time.Second

func (r *Router) SyncInterface() {
	defer liblog.PanicLog()
	for range time.Tick(IFIDFreq) {
		for ifid := range r.NetConf.IFs {
			r.GenIFIDPkt(ifid)
		}
	}
}

func (r *Router) GenIFIDPkt(ifid path.IntfID) {
	logger := log.New("ifid", ifid)
	intf := r.NetConf.IFs[ifid]
	srcAddr := intf.IFAddr.PublicAddr()
	// Create base packet
	pkt, err := packet.CreateCtrlPacket(packet.DirExternal,
		addr.HostFromIP(srcAddr.IP), intf.RemoteIA, addr.SvcBS.Multicast())
	if err != nil {
		logger.Error("Error creating IFID packet", err.Ctx...)
	}
	// Set egress
	pkt.Egress = append(pkt.Egress, packet.EgressPair{F: r.intfOutQs[ifid], Dst: intf.RemoteAddr})
	// Create IFID msg
	scion, ifidMsg, err := proto.NewIFIDMsg()
	if err != nil {
		logger.Error("Error creating IFID payload", err.Ctx...)
		return
	}
	ifidMsg.SetOrigIF(uint16(ifid))
	pkt.AddL4UDP(srcAddr.Port, 0)
	pkt.AddCtrlPld(scion)
	pkt.Route()
}
