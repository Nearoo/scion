package pqa

import (
	"context"
	"sort"

	"github.com/scionproto/scion/go/cs/beacon"
	"github.com/scionproto/scion/go/cs/ifstate"
	"github.com/scionproto/scion/go/lib/addr"
	pqa_extension "github.com/scionproto/scion/go/lib/ctrl/seg/extensions/pqabeaconing"
	"github.com/scionproto/scion/go/lib/log"
	"github.com/scionproto/scion/go/lib/serrors"
)

// Returns the propagation batch as proposed by Ali et.al. for PQA Beaconing
func (m Mechanism) getPropagationBatch(ctx context.Context, target Target, egIntfG []*ifstate.Interface, neigh addr.IA, src addr.IA) ([]beacon.Beacon, error) {
	batch := make([]beacon.Beacon, 0)
	for _, igIntfG := range m.getIntfGroups(ctx, target) {
		for _, egIntf := range egIntfG {
			for _, bcn := range m.getNBestFor(ctx, src, target, igIntfG, neigh) {
				var (
					ingress = bcn.InIfId
					egress  = egIntf.TopoInfo().ID
				)
				bcn.EgIfId = egress
				if egress == 0 {
					log.FromCtx(ctx).Error("egress interface ID is 0", "ingress", ingress, "egress", egress)
				}
				err := m.Extender.Extend(ctx, bcn.Segment, ingress, egress, nil, nil)

				if err != nil {
					return nil, serrors.WrapStr("extending beacons", err, "ingress", ingress, "egress", egress, "seg", bcn.Segment)
				}

				if target.ShouldConsider(ctx, *bcn) {
					batch = append(batch, *bcn)
				}
			}
		}
	}
	return m.getNBest(ctx, target, batch), nil
}

func (m Mechanism) getNBestFor(ctx context.Context, src addr.IA, target Target, egIntfG []*ifstate.Interface, neigh addr.IA) []*beacon.Beacon {
	bcns, err := m.GetNBestsForGroup(ctx, src, target, egIntfG, neigh)
	if err != nil {
		log.FromCtx(ctx).Error("getting n-best beacons", "err", err)
		return nil
	}
	return bcns
}

func logBcnMetrics(ctx context.Context, target Target, bcns []beacon.Beacon) {
	// logger := log.FromCtx(ctx)
	metrics := make([]float64, len(bcns))
	for i, bcn := range bcns {
		metrics[i] = target.GetMetric(ctx, bcn)
	}
	// logger.Info("got beacons for target", "metrics", metrics, "target", target)
}

func (m Mechanism) getNBest(ctx context.Context, target Target, bcn []beacon.Beacon) []beacon.Beacon {
	//logger := log.FromCtx(ctx)

	logBcnMetrics(ctx, target, bcn)

	// Compares beacons i.t.o. their metric value for target
	less := func(l, r int) bool {
		lBcn, rBcn := bcn[l], bcn[r]
		lMet, rMet := target.GetMetric(ctx, lBcn), target.GetMetric(ctx, rBcn)
		return target.Quality.Less(lMet, rMet)
	}
	sort.Slice(bcn, less)

	if len(bcn) > pqa_extension.N {
		return bcn[:pqa_extension.N]
	}

	return bcn[:]
}

func (m Mechanism) getSourceIAs(ctx context.Context) []addr.IA {
	addrs, err := m.BeaconSources(ctx)
	if err != nil {
		log.FromCtx(ctx).Error("getting beacon sources", "err", err)
	}

	return addrs
}

func (m Mechanism) getTargetsFromReceivedBeacons(ctx context.Context, srcIA addr.IA) []Target {
	targets, err := m.GetActiveTargets(ctx, srcIA)
	if err != nil {
		log.FromCtx(ctx).Error("error getting active targets", "err", err)
	}
	return targets
}

func (m Mechanism) getNeighbouringASs(ctx context.Context) []addr.IA {
	// Find set of all IAs contained in AllInterfaces
	set := make(map[addr.IA]bool)
	for _, neigh := range m.AllInterfaces.All() {
		set[neigh.TopoInfo().IA] = true
	}
	// Turn to list
	res := make([]addr.IA, len(set))
	for neigh := range set {
		res = append(res, neigh)
	}
	return res
}

func (m Mechanism) getIntfSubgroups(ctx context.Context, target Target, to addr.IA) [][]*ifstate.Interface {
	res := make([][]*ifstate.Interface, 0)
	for _, intfG := range m.getIntfGroups(ctx, target) {
		intfSubg := make([]*ifstate.Interface, 0)
		for _, intf := range intfG {
			if intf.TopoInfo().IA == to {
				intfSubg = append(intfSubg, intf)
			}
		}
		res = append(res, intfSubg)
	}
	return res
}

func (m Mechanism) getIntfGroups(ctx context.Context, target Target) [][]*ifstate.Interface {
	return m.GetInterfaceGroups(target.Quality, target.Direction)[:]
}