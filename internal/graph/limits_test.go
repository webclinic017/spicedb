package graph

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLimitsPrepareForPublishing(t *testing.T) {
	limits, ctx := newLimitTracker(context.Background(), 10)

	for i := 0; i < 10; i++ {
		result, done := limits.prepareForPublishing()
		done()

		require.True(t, result)
		if i == 9 {
			require.NotNil(t, ctx.Err())
		} else {
			require.Nil(t, ctx.Err())
		}
	}

	result, done := limits.prepareForPublishing()
	done()

	require.False(t, result)
}

func TestLimitsMarkAlreadyPublished(t *testing.T) {
	limits, _ := newLimitTracker(context.Background(), 10)

	_, err := limits.markAlreadyPublished(5)
	require.Nil(t, err)

	_, err = limits.markAlreadyPublished(5)
	require.Nil(t, err)

	require.Panics(t, func() {
		_, _ = limits.markAlreadyPublished(1)
	})
}
