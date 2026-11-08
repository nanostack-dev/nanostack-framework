package secret

import "testing"

// Every suffix is in the bucket of its last byte, so IsKey, which looks
// only in that bucket, finds each one.
func TestEverySuffixIsInItsBucket(t *testing.T) {
	t.Parallel()
	for _, suffix := range secretSuffixes {
		found := false
		for _, s := range suffixesByLastByte[suffix[len(suffix)-1]] {
			found = found || s == suffix
		}
		if !found {
			t.Errorf("suffix %q is not in the bucket of %q", suffix, suffix[len(suffix)-1])
		}
		if !IsKey(suffix) {
			t.Errorf("IsKey(%q) = false", suffix)
		}
	}
}
