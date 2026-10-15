package review

import (
	"context"
	"errors"
	"testing"
)

func TestEmptyFeatureBranchIsNotAnAmbiguousStart(t *testing.T) {
	f := newFixture(t)
	f.git("checkout", "-b", "feature")
	base := f.git("rev-parse", "HEAD")
	for _, reset := range []bool{false, true} {
		if reset {
			f.commit("temporary test", map[string]string{"temporary.txt": "test\n"})
			f.git("reset", "--hard", base)
		}
		_, err := DetectScope(context.Background(), f.path, "feature")
		if !errors.Is(err, ErrNoFeatureChanges) {
			t.Fatalf("empty branch should not ask for manual history: %v", err)
		}
	}
}
