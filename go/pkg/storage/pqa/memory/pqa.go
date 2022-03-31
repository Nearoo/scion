package memory

import (
	"context"
	"sort"

	"github.com/scionproto/scion/go/cs/beacon"
	"github.com/scionproto/scion/go/cs/beaconing/mechanisms/pqa"
	"github.com/scionproto/scion/go/cs/ifstate"
	"github.com/scionproto/scion/go/lib/addr"
	pqa_extension "github.com/scionproto/scion/go/lib/ctrl/seg/extensions/pqabeaconing"
	"github.com/scionproto/scion/go/lib/log"
	targetstore "github.com/scionproto/scion/go/pkg/storage/pqa/memory/target"
)

type PqaMemoryBackend struct {
	*Beacons
	*targetstore.Targets
}

func New(path string, ia addr.IA) (*PqaMemoryBackend, error) {
	beaconDB, err := NewBeaconBackend(path, ia)
	if err != nil {
		return nil, err
	}
	targetDB, err := targetstore.NewTargetBackend()
	if err != nil {
		return nil, err
	}

	return &PqaMemoryBackend{
		Beacons: beaconDB,
		Targets: targetDB,
	}, nil
}

func (b *PqaMemoryBackend) InsertBeacon(
	ctx context.Context,
	bcn beacon.Beacon,
	usage beacon.Usage,
) (beacon.InsertStats, error) {

	// Store beacon in normal beacon storage
	stats, err := b.Beacons.InsertBeacon(ctx, bcn, usage)
	if err != nil {
		return stats, err
	}

	logger := log.FromCtx(ctx)
	// If beacon has no pqaExtension, return
	pqaExtension0 := bcn.Segment.ASEntries[0].Extensions.PqaExtension
	if pqaExtension0 == nil {
		logger.Debug("Inserted beacon without extension.")
		return stats, nil
	}
	logger.Debug("Got beacon with extension")

	target := pqa.Target{
		Quality:    pqaExtension0.Quality,
		Direction:  pqaExtension0.Direction,
		Uniquifier: uint32(pqaExtension0.Uniquifier),
		IA:         bcn.Segment.FirstIA(),
	}

	beaconID, err := b.Beacons.getBeaconID(ctx, bcn)
	if err != nil {
		return stats, err
	}

	b.Targets.AssociateBeacon(beaconID, target)
	return stats, nil
}

func (b *PqaMemoryBackend) GetNBestsForGroup(
	ctx context.Context,
	src addr.IA,
	target pqa.Target,
	ingressIntfs []*ifstate.Interface,
	excludeLooping addr.IA,
) ([]*beacon.Beacon, error) {

	// Get beaconIds that are associated with the target
	bcnIds := b.Targets.GetBeaconIdsForTarget(ctx, target)
	if len(bcnIds) == 0 {
		return nil, nil
	}

	// Get beacons from beacon ids
	bcnCandidates, err := b.Beacons.GetBeaconsById(ctx, bcnIds)
	if err != nil {
		log.FromCtx(ctx).Error("Fetching beacons", "error", err)
		// return nil, err
	}

	// BeaconFilter is a predicate function beacons, keep iff true
	type BeaconFilter func(*beacon.Beacon) bool
	// applyFilter filters beacons based on a BeaconFilter function
	applyFilter := func(bcns []*beacon.Beacon, filter BeaconFilter) []*beacon.Beacon {
		var filtered []*beacon.Beacon
		for _, bcn := range bcns {
			if filter(bcn) {
				filtered = append(filtered, bcn)
			}
		}
		return filtered
	}

	// Only keep beacons that entered AS through an interface in ingressIntfs
	bcnCandidates = applyFilter(bcnCandidates, func(bcn *beacon.Beacon) bool {
		for _, ingressIntf := range ingressIntfs {
			ingressIfid := ingressIntf.TopoInfo().ID
			if ingressIfid == bcn.InIfId {
				return true
			}
		}
		return false
	})

	// Only keep beacons that would not loop if sent to excludeLooping
	bcnCandidates = applyFilter(bcnCandidates, func(bcn *beacon.Beacon) bool {
		if err := beacon.FilterLoop(*bcn, excludeLooping, true); err != nil {
			return false
		}
		return true
	})

	// Only keep beacons that return true by ShouldConsider predicate of target
	bcnCandidates = applyFilter(bcnCandidates, func(bcn *beacon.Beacon) bool {
		return target.ShouldConsider(ctx, *bcn)
	})

	// Compares beacons i.t.o. their metric value for target
	less := func(l, r int) bool {
		lBcn, rBcn := bcnCandidates[l], bcnCandidates[r]
		lMet, rMet := target.GetMetric(ctx, *lBcn), target.GetMetric(ctx, *rBcn)
		return target.Quality.Less(lMet, rMet)
	}
	// Sort beacons by quality metric
	sort.Slice(bcnCandidates, less)

	// Debug: Show beacon metrics
	// logger := log.FromCtx(ctx)
	// logger.Debug("Beacon metrics:")
	// for i, bcn := range bcnCandidates {
	// 	logger.Debug("Metric", "beacon index", i, "metric", target.GetMetric(ctx, bcn), "quality", target.Quality)
	// }

	// Return up to the first n beacons
	if len(bcnCandidates) > pqa_extension.N {
		bcnCandidates = bcnCandidates[:pqa_extension.N]
	}
	return bcnCandidates, nil
}