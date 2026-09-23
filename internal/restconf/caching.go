package restconf

import (
	"context"
	"fmt"
	"hash/fnv"
	"net/http"
	"strings"
	"time"

	"github.com/DisMosGit/triadsim/internal/restconf/ops"
	"github.com/DisMosGit/triadsim/internal/store"
)

// Caching metadata and conditional requests (RFC 8040 §3.4.1, §3.5.1,
// §3.5.2 and §5.5).
//
// The datastore resource and every data resource carry an entity-tag and a
// last-modification time, and the server honors If-Match / If-Unmodified-Since
// on edits and If-None-Match / If-Modified-Since on retrievals. Both
// validators move only when configuration data changes: RFC 8040 §3.4.1.1 and
// §3.4.1.2 forbid them from reacting to state-data writes, which is what the
// store's change tracking (Generation vs ConfigChangedAt) records.

// validators returns the entity-tag and the last-modification time of ds for
// one representation. RFC 8040 §3.4.1.2 requires a different entity-tag per
// representation, so the format is part of the hashed material, as are the
// datastore and the time of its last configuration change. The
// last-modification time is zero when no configuration change has been
// recorded yet.
func (s *Server) validators(ctx context.Context, ds store.Datastore, format ops.Format) (string, time.Time, error) {
	changed, err := s.store.ConfigChangedAt(ctx, ds)
	if err != nil {
		return "", time.Time{}, err
	}
	sum := fnv.New64a()
	fmt.Fprintf(sum, "%s|%s|%d", ds, format, changed.UnixNano())
	return fmt.Sprintf(`"%016x"`, sum.Sum64()), changed, nil
}

// setValidators writes the caching metadata of a resource (RFC 8040 §3.5.1
// and §3.5.2): its entity-tag and, when known, its last-modification time.
func setValidators(w http.ResponseWriter, etag string, changed time.Time) {
	w.Header().Set("ETag", etag)
	if !changed.IsZero() {
		w.Header().Set("Last-Modified", changed.UTC().Format(http.TimeFormat))
	}
}

// checkPreconditions evaluates the RFC 7232 conditional headers of an edit
// and returns the 412 response when the client edited a stale copy (RFC 8040
// §3.4.1). If-Match wins over If-Unmodified-Since. An edit does not declare a
// representation, so an If-Match entity-tag is accepted from either
// representation's tag.
func (s *Server) checkPreconditions(w http.ResponseWriter, r *http.Request, ds store.Datastore) *httpError {
	if r.Header.Get("If-Match") == "" && r.Header.Get("If-Unmodified-Since") == "" {
		return nil
	}

	ctx := r.Context()
	etagJSON, changed, err := s.validators(ctx, ds, ops.FormatJSON)
	if err != nil {
		return internalError(err)
	}
	etagXML, _, err := s.validators(ctx, ds, ops.FormatXML)
	if err != nil {
		return internalError(err)
	}

	failed := false
	if header := r.Header.Get("If-Match"); header != "" {
		failed = !etagMatches(header, etagJSON) && !etagMatches(header, etagXML)
	} else {
		failed = modifiedAfter(r.Header.Get("If-Unmodified-Since"), changed)
	}
	if !failed {
		return nil
	}

	setValidators(w, etagJSON, changed)
	return preconditionFailed("the datastore changed since the entity-tag or timestamp in the request")
}

// notModified reports whether the retrieval precondition of r says the
// client's copy is current (RFC 7232 §3). If-None-Match wins over
// If-Modified-Since.
func notModified(r *http.Request, etag string, changed time.Time) bool {
	if header := r.Header.Get("If-None-Match"); header != "" {
		return etagMatches(header, etag)
	}
	return notModifiedSince(r.Header.Get("If-Modified-Since"), changed)
}

// etagMatches reports whether header — an RFC 7232 entity-tag list, or "*"
// for any current representation — includes etag. The simulator only issues
// strong entity-tags, so the weak comparison never differs from the strong
// one.
func etagMatches(header, etag string) bool {
	if strings.TrimSpace(header) == "*" {
		return true
	}
	for _, candidate := range strings.Split(header, ",") {
		if strings.TrimSpace(candidate) == etag {
			return true
		}
	}
	return false
}

// notModifiedSince reports whether changed is not after the HTTP-date in
// header, that is, the client's copy is current. HTTP dates have second
// granularity (RFC 7232 §2.2), so both sides compare truncated to a second.
// An unparseable date never satisfies the condition.
func notModifiedSince(header string, changed time.Time) bool {
	if header == "" {
		return false
	}
	since, err := http.ParseTime(header)
	if err != nil {
		return false
	}
	return !changed.Truncate(time.Second).After(since)
}

// modifiedAfter reports whether changed is after the HTTP-date in header,
// that is, the client edited a stale copy. An unparseable date never fails
// the condition.
func modifiedAfter(header string, changed time.Time) bool {
	if header == "" {
		return false
	}
	since, err := http.ParseTime(header)
	if err != nil {
		return false
	}
	return changed.Truncate(time.Second).After(since)
}

// withCacheControl stamps every response with the Cache-Control header RFC
// 8040 §5.5 requires; datastores change at unpredictable times, so nothing is
// cacheable and clients track the entity-tag instead.
func withCacheControl(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}
