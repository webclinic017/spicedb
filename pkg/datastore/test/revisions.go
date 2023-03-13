package test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/authzed/spicedb/internal/datastore/common"
	"github.com/authzed/spicedb/pkg/datastore"
	core "github.com/authzed/spicedb/pkg/proto/core/v1"
	dispatch "github.com/authzed/spicedb/pkg/proto/dispatch/v1"
)

// RevisionQuantizationTest tests whether or not the requirements for revisions hold
// for a particular datastore.
func RevisionQuantizationTest(t *testing.T, tester DatastoreTester) {
	testCases := []struct {
		quantizationRange        time.Duration
		expectFindLowerRevisions bool
	}{
		{0 * time.Second, false},
		{100 * time.Millisecond, true},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("quantization%s", tc.quantizationRange), func(t *testing.T) {
			require := require.New(t)

			ds, err := tester.New(tc.quantizationRange, veryLargeGCInterval, veryLargeGCWindow, 1)
			require.NoError(err)

			ctx := context.Background()
			veryFirstRevision, err := ds.OptimizedRevision(ctx)
			require.NoError(err)
			require.True(veryFirstRevision.GreaterThan(datastore.NoRevision))

			postSetupRevision := setupDatastore(ds, require)
			require.True(postSetupRevision.GreaterThan(veryFirstRevision))

			// Create some revisions
			var writtenAt datastore.Revision
			tpl := makeTestTuple("first", "owner")
			for i := 0; i < 10; i++ {
				writtenAt, err = common.WriteTuples(ctx, ds, core.RelationTupleUpdate_TOUCH, tpl)
				require.NoError(err)
			}
			require.True(writtenAt.GreaterThan(postSetupRevision))

			// Get the new now revision
			nowRevision, err := ds.HeadRevision(ctx)
			require.NoError(err)
			require.True(nowRevision.GreaterThan(datastore.NoRevision))

			// Let the quantization window expire
			time.Sleep(tc.quantizationRange)

			// Now we should ONLY get revisions later than the now revision
			for start := time.Now(); time.Since(start) < 10*time.Millisecond; {
				testRevision, err := ds.OptimizedRevision(ctx)
				require.NoError(err)
				require.True(nowRevision.LessThan(testRevision) || nowRevision.Equal(testRevision))
			}
		})
	}
}

// RevisionSerializationTest tests whether the revisions generated by this datastore can
// be serialized and sent through the dispatch layer.
func RevisionSerializationTest(t *testing.T, tester DatastoreTester) {
	require := require.New(t)

	ds, err := tester.New(0, veryLargeGCInterval, veryLargeGCWindow, 1)
	require.NoError(err)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	revToTest, err := ds.ReadWriteTx(ctx, func(rwt datastore.ReadWriteTransaction) error {
		return rwt.WriteNamespaces(ctx, testNamespace)
	})
	require.NoError(err)

	meta := dispatch.ResolverMeta{
		AtRevision:     revToTest.String(),
		DepthRemaining: 50,
	}
	require.NoError(meta.Validate())
}

// RevisionGCTest makes sure revision GC takes place, revisions out-side of the GC window
// are invalid, and revisions inside the GC window are valid.
func RevisionGCTest(t *testing.T, tester DatastoreTester) {
	require := require.New(t)

	ds, err := tester.New(0, 10*time.Millisecond, 300*time.Millisecond, 1)
	require.NoError(err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	previousRev, err := ds.ReadWriteTx(ctx, func(rwt datastore.ReadWriteTransaction) error {
		return rwt.WriteNamespaces(ctx, testNamespace)
	})
	require.NoError(err)
	require.NoError(ds.CheckRevision(ctx, previousRev), "expected latest write revision to be within GC window")

	head, err := ds.HeadRevision(ctx)
	require.NoError(err)
	require.NoError(ds.CheckRevision(ctx, head), "expected head revision to be valid in GC Window")

	// wait to make sure GC kicks in
	time.Sleep(400 * time.Millisecond)

	// check freshly fetched head revision is valid after GC window elapsed
	head, err = ds.HeadRevision(ctx)
	require.NoError(err)

	_, _, err = ds.SnapshotReader(head).ReadNamespaceByName(ctx, "foo/bar")
	require.NoError(err, "expected previously written schema to exist at head")
	require.NoError(ds.CheckRevision(ctx, head), "expected freshly obtained head revision to be valid")

	newerRev, err := ds.ReadWriteTx(ctx, func(rwt datastore.ReadWriteTransaction) error {
		return rwt.WriteNamespaces(ctx, testNamespace)
	})
	require.NoError(err)
	require.NoError(ds.CheckRevision(ctx, newerRev), "expected newer head revision to be within GC Window")
	require.Error(ds.CheckRevision(ctx, previousRev), "expected revision head-1 to be outside GC Window")
}
