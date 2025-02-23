//go:build go1.18

package sync

import (
	"bytes"
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pb "github.com/libp2p/go-libp2p-pubsub/pb"
	gcache "github.com/patrickmn/go-cache"
	mock "github.com/prysmaticlabs/prysm/beacon-chain/blockchain/testing"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/signing"
	dbtest "github.com/prysmaticlabs/prysm/beacon-chain/db/testing"
	"github.com/prysmaticlabs/prysm/beacon-chain/p2p"
	p2ptest "github.com/prysmaticlabs/prysm/beacon-chain/p2p/testing"
	"github.com/prysmaticlabs/prysm/beacon-chain/state/stategen"
	mockSync "github.com/prysmaticlabs/prysm/beacon-chain/sync/initial-sync/testing"
	lruwrpr "github.com/prysmaticlabs/prysm/cache/lru"
	"github.com/prysmaticlabs/prysm/config/params"
	ethpb "github.com/prysmaticlabs/prysm/proto/prysm/v1alpha1"
	"github.com/prysmaticlabs/prysm/testing/assert"
	"github.com/prysmaticlabs/prysm/testing/require"
	"github.com/prysmaticlabs/prysm/testing/util"
)

func FuzzValidateBeaconBlockPubSub_Phase0(f *testing.F) {
	db := dbtest.SetupDB(f)
	p := p2ptest.NewFuzzTestP2P()
	ctx := context.Background()
	beaconState, privKeys := util.DeterministicGenesisState(f, 100)
	parentBlock := util.NewBeaconBlock()
	util.SaveBlock(f, ctx, db, parentBlock)
	bRoot, err := parentBlock.Block.HashTreeRoot()
	require.NoError(f, err)
	require.NoError(f, db.SaveState(ctx, beaconState, bRoot))
	require.NoError(f, db.SaveStateSummary(ctx, &ethpb.StateSummary{Root: bRoot[:]}))
	copied := beaconState.Copy()
	require.NoError(f, copied.SetSlot(1))
	proposerIdx, err := helpers.BeaconProposerIndex(ctx, copied)
	require.NoError(f, err)
	msg := util.NewBeaconBlock()
	msg.Block.ParentRoot = bRoot[:]
	msg.Block.Slot = 1
	msg.Block.ProposerIndex = proposerIdx
	msg.Signature, err = signing.ComputeDomainAndSign(beaconState, 0, msg.Block, params.BeaconConfig().DomainBeaconProposer, privKeys[proposerIdx])
	require.NoError(f, err)

	stateGen := stategen.New(db)
	chainService := &mock.ChainService{Genesis: time.Unix(time.Now().Unix()-int64(params.BeaconConfig().SecondsPerSlot), 0),
		State: beaconState,
		FinalizedCheckPoint: &ethpb.Checkpoint{
			Epoch: 0,
			Root:  make([]byte, 32),
		},
		DB: db,
	}
	r := &Service{
		cfg: &config{
			beaconDB:      db,
			p2p:           p,
			initialSync:   &mockSync.Sync{IsSyncing: false},
			chain:         chainService,
			blockNotifier: chainService.BlockNotifier(),
			stateGen:      stateGen,
		},
		seenBlockCache:      lruwrpr.New(10),
		badBlockCache:       lruwrpr.New(10),
		slotToPendingBlocks: gcache.New(time.Second, 2*time.Second),
		seenPendingBlocks:   make(map[[32]byte]bool),
	}
	buf := new(bytes.Buffer)
	_, err = p.Encoding().EncodeGossip(buf, msg)
	require.NoError(f, err)
	topic := p2p.GossipTypeMapping[reflect.TypeOf(msg)]
	digest, err := r.currentForkDigest()
	assert.NoError(f, err)
	topic = r.addDigestToTopic(topic, digest)

	f.Add("junk", []byte("junk"), buf.Bytes(), []byte(topic))
	f.Fuzz(func(t *testing.T, pid string, from, data, topic []byte) {
		r.cfg.p2p = p2ptest.NewFuzzTestP2P()
		r.rateLimiter = newRateLimiter(r.cfg.p2p)
		cService := &mock.ChainService{
			Genesis: time.Unix(time.Now().Unix()-int64(params.BeaconConfig().SecondsPerSlot*10000000), 0),
			State:   beaconState,
			FinalizedCheckPoint: &ethpb.Checkpoint{
				Epoch: 0,
				Root:  make([]byte, 32),
			},
			DB: db,
		}
		r.cfg.chain = cService
		r.cfg.blockNotifier = cService.BlockNotifier()
		strTop := string(topic)
		msg := &pubsub.Message{
			Message: &pb.Message{
				From:  from,
				Data:  data,
				Topic: &strTop,
			},
		}
		_, err := r.validateBeaconBlockPubSub(ctx, peer.ID(pid), msg)
		_ = err
	})
}

func FuzzValidateBeaconBlockPubSub_Altair(f *testing.F) {
	db := dbtest.SetupDB(f)
	p := p2ptest.NewFuzzTestP2P()
	ctx := context.Background()
	beaconState, privKeys := util.DeterministicGenesisStateAltair(f, 100)
	parentBlock := util.NewBeaconBlockAltair()
	util.SaveBlock(f, ctx, db, parentBlock)
	bRoot, err := parentBlock.Block.HashTreeRoot()
	require.NoError(f, err)
	require.NoError(f, db.SaveState(ctx, beaconState, bRoot))
	require.NoError(f, db.SaveStateSummary(ctx, &ethpb.StateSummary{Root: bRoot[:]}))
	copied := beaconState.Copy()
	require.NoError(f, copied.SetSlot(1))
	proposerIdx, err := helpers.BeaconProposerIndex(ctx, copied)
	require.NoError(f, err)
	msg := util.NewBeaconBlock()
	msg.Block.ParentRoot = bRoot[:]
	msg.Block.Slot = 1
	msg.Block.ProposerIndex = proposerIdx
	msg.Signature, err = signing.ComputeDomainAndSign(beaconState, 0, msg.Block, params.BeaconConfig().DomainBeaconProposer, privKeys[proposerIdx])
	require.NoError(f, err)

	stateGen := stategen.New(db)
	chainService := &mock.ChainService{Genesis: time.Unix(time.Now().Unix()-int64(params.BeaconConfig().SecondsPerSlot), 0),
		State: beaconState,
		FinalizedCheckPoint: &ethpb.Checkpoint{
			Epoch: 0,
			Root:  make([]byte, 32),
		},
		DB: db,
	}
	r := &Service{
		cfg: &config{
			beaconDB:      db,
			p2p:           p,
			initialSync:   &mockSync.Sync{IsSyncing: false},
			chain:         chainService,
			blockNotifier: chainService.BlockNotifier(),
			stateGen:      stateGen,
		},
		seenBlockCache:      lruwrpr.New(10),
		badBlockCache:       lruwrpr.New(10),
		slotToPendingBlocks: gcache.New(time.Second, 2*time.Second),
		seenPendingBlocks:   make(map[[32]byte]bool),
	}
	buf := new(bytes.Buffer)
	_, err = p.Encoding().EncodeGossip(buf, msg)
	require.NoError(f, err)
	topic := p2p.GossipTypeMapping[reflect.TypeOf(msg)]
	digest, err := r.currentForkDigest()
	assert.NoError(f, err)
	topic = r.addDigestToTopic(topic, digest)

	f.Add("junk", []byte("junk"), buf.Bytes(), []byte(topic))
	f.Fuzz(func(t *testing.T, pid string, from, data, topic []byte) {
		r.cfg.p2p = p2ptest.NewFuzzTestP2P()
		r.rateLimiter = newRateLimiter(r.cfg.p2p)
		cService := &mock.ChainService{
			Genesis: time.Unix(time.Now().Unix()-int64(params.BeaconConfig().SecondsPerSlot*10000000), 0),
			State:   beaconState,
			FinalizedCheckPoint: &ethpb.Checkpoint{
				Epoch: 0,
				Root:  make([]byte, 32),
			},
			DB: db,
		}
		r.cfg.chain = cService
		r.cfg.blockNotifier = cService.BlockNotifier()
		strTop := string(topic)
		msg := &pubsub.Message{
			Message: &pb.Message{
				From:  from,
				Data:  data,
				Topic: &strTop,
			},
		}
		_, err := r.validateBeaconBlockPubSub(ctx, peer.ID(pid), msg)
		_ = err
	})
}

func FuzzValidateBeaconBlockPubSub_Bellatrix(f *testing.F) {
	db := dbtest.SetupDB(f)
	p := p2ptest.NewFuzzTestP2P()
	ctx := context.Background()
	beaconState, privKeys := util.DeterministicGenesisStateBellatrix(f, 100)
	parentBlock := util.NewBeaconBlockBellatrix()
	util.SaveBlock(f, ctx, db, parentBlock)
	bRoot, err := parentBlock.Block.HashTreeRoot()
	require.NoError(f, err)
	require.NoError(f, db.SaveState(ctx, beaconState, bRoot))
	require.NoError(f, db.SaveStateSummary(ctx, &ethpb.StateSummary{Root: bRoot[:]}))
	copied := beaconState.Copy()
	require.NoError(f, copied.SetSlot(1))
	proposerIdx, err := helpers.BeaconProposerIndex(ctx, copied)
	require.NoError(f, err)
	msg := util.NewBeaconBlock()
	msg.Block.ParentRoot = bRoot[:]
	msg.Block.Slot = 1
	msg.Block.ProposerIndex = proposerIdx
	msg.Signature, err = signing.ComputeDomainAndSign(beaconState, 0, msg.Block, params.BeaconConfig().DomainBeaconProposer, privKeys[proposerIdx])
	require.NoError(f, err)

	stateGen := stategen.New(db)
	chainService := &mock.ChainService{Genesis: time.Unix(time.Now().Unix()-int64(params.BeaconConfig().SecondsPerSlot), 0),
		State: beaconState,
		FinalizedCheckPoint: &ethpb.Checkpoint{
			Epoch: 0,
			Root:  make([]byte, 32),
		},
		DB: db,
	}
	r := &Service{
		cfg: &config{
			beaconDB:      db,
			p2p:           p,
			initialSync:   &mockSync.Sync{IsSyncing: false},
			chain:         chainService,
			blockNotifier: chainService.BlockNotifier(),
			stateGen:      stateGen,
		},
		seenBlockCache:      lruwrpr.New(10),
		badBlockCache:       lruwrpr.New(10),
		slotToPendingBlocks: gcache.New(time.Second, 2*time.Second),
		seenPendingBlocks:   make(map[[32]byte]bool),
	}
	buf := new(bytes.Buffer)
	_, err = p.Encoding().EncodeGossip(buf, msg)
	require.NoError(f, err)
	topic := p2p.GossipTypeMapping[reflect.TypeOf(msg)]
	digest, err := r.currentForkDigest()
	assert.NoError(f, err)
	topic = r.addDigestToTopic(topic, digest)

	f.Add("junk", []byte("junk"), buf.Bytes(), []byte(topic))
	f.Fuzz(func(t *testing.T, pid string, from, data, topic []byte) {
		r.cfg.p2p = p2ptest.NewFuzzTestP2P()
		r.rateLimiter = newRateLimiter(r.cfg.p2p)
		cService := &mock.ChainService{
			Genesis: time.Unix(time.Now().Unix()-int64(params.BeaconConfig().SecondsPerSlot*10000000), 0),
			State:   beaconState,
			FinalizedCheckPoint: &ethpb.Checkpoint{
				Epoch: 0,
				Root:  make([]byte, 32),
			},
			DB: db,
		}
		r.cfg.chain = cService
		r.cfg.blockNotifier = cService.BlockNotifier()
		strTop := string(topic)
		msg := &pubsub.Message{
			Message: &pb.Message{
				From:  from,
				Data:  data,
				Topic: &strTop,
			},
		}
		_, err := r.validateBeaconBlockPubSub(ctx, peer.ID(pid), msg)
		_ = err
	})
}
