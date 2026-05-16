package discovery

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/discovery/core"
	discoveryTypes "github.com/gnmic/operator/internal/controller/discovery/core"
	"github.com/go-logr/logr"
	"github.com/go-openapi/testify/v2/require"
)

func mockMessageProcessor(opts ...func(*MessageProcessor)) *MessageProcessor {
	bufferSize := 10

	scheme := runtime.NewScheme()

	// Register built-in k8s types
	_ = clientgoscheme.AddToScheme(scheme)

	// Register your CRDs
	_ = gnmicv1alpha1.AddToScheme(scheme)

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	targetSource := mockTargetSource()
	targetChannel := make(chan []discoveryTypes.DiscoveryMessage, bufferSize)

	m := NewMessageProcessor(
		client,
		scheme,
		&targetSource,
		targetChannel,
	)

	for _, opt := range opts {
		opt(m)
	}

	return m
}

func withTargetChannel(ch <-chan []discoveryTypes.DiscoveryMessage) func(*MessageProcessor) {
	return func(m *MessageProcessor) {
		m.in = ch
	}
}

func TestRun_StopsWhenChannelClosed(t *testing.T) {
	ch := make(chan []discoveryTypes.DiscoveryMessage, 10)

	m := mockMessageProcessor(
		withTargetChannel(ch),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)

	go func() {
		errCh <- m.Run(ctx)
	}()

	close(ch)

	select {
	case err := <-errCh:
		require.NoError(t, err)

	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after channel close")
	}
}

func TestRun_StopsWhenContextCanceled(t *testing.T) {
	m := mockMessageProcessor()

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)

	go func() {
		errCh <- m.Run(ctx)
	}()

	cancel()

	select {
	case err := <-errCh:
		require.NoError(t, err)

	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after context cancel")
	}
}

func TestProcessEvent_DefersDuringSnapshot(t *testing.T) {
	m := mockMessageProcessor()

	m.activeSnapshot = &snapshotBuffer{
		snapshotID:  "snap-1",
		totalChunks: 1,
		received:    map[int][]core.DiscoveredTarget{},
	}

	event := core.DiscoveryEvent{
		Event: core.EventApply,
		Target: core.DiscoveredTarget{
			Name: "router-1",
		},
	}

	err := m.processEvent(
		context.Background(),
		event,
		logr.Discard(),
	)

	require.NoError(t, err)

	require.Len(t, m.deferredEvents, 1)
	require.Equal(t, "router-1", m.deferredEvents[0].Target.Name)
}

func TestStartNewSnapshot_ResetsDeferredEvents(t *testing.T) {
	m := mockMessageProcessor()

	m.deferredEvents = []core.DiscoveryEvent{
		{
			Event: core.EventApply,
		},
	}

	chunk := core.DiscoverySnapshot{
		SnapshotID:  "snap-1",
		TotalChunks: 1,
		ChunkIndex:  0,
		Targets: []core.DiscoveredTarget{
			{
				Name: "router-1",
			},
		},
	}

	err := m.startNewSnapshot(
		context.Background(),
		chunk,
		logr.Discard(),
	)

	require.NoError(t, err)

	require.Nil(t, m.deferredEvents)
	require.Nil(t, m.activeSnapshot)
}

func TestCollectSnapshot_DuplicateChunkFails(t *testing.T) {
	m := mockMessageProcessor()

	m.activeSnapshot = &snapshotBuffer{
		snapshotID:  "snap-1",
		totalChunks: 2,
		received: map[int][]core.DiscoveredTarget{
			0: {},
		},
	}

	chunk := core.DiscoverySnapshot{
		SnapshotID:  "snap-1",
		TotalChunks: 2,
		ChunkIndex:  0,
	}

	err := m.collectSnapshot(
		context.Background(),
		chunk,
		logr.Discard(),
	)

	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate snapshot chunk")

	require.Nil(t, m.activeSnapshot)
}

func TestCollectSnapshot_InvalidChunkIndexFails(t *testing.T) {
	m := mockMessageProcessor()

	m.activeSnapshot = &snapshotBuffer{
		snapshotID:  "snap-1",
		totalChunks: 1,
		received:    map[int][]core.DiscoveredTarget{},
	}

	chunk := core.DiscoverySnapshot{
		SnapshotID:  "snap-1",
		TotalChunks: 1,
		ChunkIndex:  99,
	}

	err := m.collectSnapshot(
		context.Background(),
		chunk,
		logr.Discard(),
	)

	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid chunk index")

	require.Nil(t, m.activeSnapshot)
}

func TestProcessSnapshot_StartsNewSnapshot(t *testing.T) {
	mp := mockMessageProcessor()

	chunk := core.DiscoverySnapshot{
		SnapshotID:  "snap-1",
		TotalChunks: 2,
		ChunkIndex:  0,
		Targets: []core.DiscoveredTarget{
			{
				Name: "router-1",
			},
		},
	}

	err := mp.processSnapshot(
		context.Background(),
		chunk,
		logr.Discard(),
	)

	require.NoError(t, err)

	require.NotNil(t, mp.activeSnapshot)
	require.Equal(t, "snap-1", mp.activeSnapshot.snapshotID)
	require.Len(t, mp.activeSnapshot.received, 1)
}

func TestResetSnapshot(t *testing.T) {
	mp := mockMessageProcessor()

	mp.activeSnapshot = &snapshotBuffer{
		snapshotID: "snap-1",
	}

	mp.resetSnapshot()

	require.Nil(t, mp.activeSnapshot)
}
