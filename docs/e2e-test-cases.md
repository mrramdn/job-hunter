# Finder end-to-end test cases

These cases cover `finder.Client.Search` against LinkedIn's live guest
endpoints. The automated ones live in `finder/e2e_test.go` behind the `e2e`
build tag:

```sh
go test -tags e2e ./finder
```

LinkedIn data changes constantly, so every case asserts invariants (shape,
limits, filters sent) rather than exact jobs. Keep `ResultsWanted` small:
each extra search page waits 3–7 seconds, and detail requests for
`FetchDescription` are sent back to back without a delay.

## Summary

| ID | Case | Priority | Status |
| --- | --- | --- | --- |
| E2E-01 | Basic search returns well-formed jobs | P0 | Automated (`TestSearch`) |
| E2E-02 | `FetchDescription` fills descriptions | P0 | Automated (`TestSearch_FetchesDescription`) |
| E2E-03 | Result cap, pagination, and unique IDs | P0 | Proposed |
| E2E-04 | `Offset` starts at the requested page | P2 | Proposed |
| E2E-05 | Multiple locations are searched round-robin | P1 | Proposed |
| E2E-06 | Locations are normalized | P2 | Proposed |
| E2E-07 | `HoursOld` filters by posting age | P1 | Proposed |
| E2E-08 | `RemoteOnly` sends the remote filter | P2 | Proposed |
| E2E-09 | `JobType` filters employment type | P2 | Proposed |
| E2E-10 | `CompanyIDs` filters by company | P1 | Proposed |
| E2E-11 | Description formats | P1 | Proposed |
| E2E-12 | Detail-page metadata | P2 | Proposed |
| E2E-13 | Invalid options fail without HTTP requests | P0 | Proposed |
| E2E-14 | Context cancellation stops the search | P1 | Proposed |
| E2E-15 | Query with no matches | P2 | Proposed |
| E2E-16 | Rate limiting returns `ErrRateLimited` | P1 | Unit test only |

## Invariants for every returned job

Every case that returns jobs should also check these:

- `ID` matches `^li-[0-9]+$`, and IDs are unique within one result.
- `JobURL` is `https://www.linkedin.com/jobs/view/` + `ID` without `li-`.
- `Title` and `CompanyName` are not `"N/A"`. The parser uses `"N/A"` when a
  selector finds nothing, so an empty-string check can never fail.
- `CompanyURL`, when set, has no query string.
- `len(jobs) <= ResultsWanted`.
- `Compensation`, when set, has `0 < MinAmount <= MaxAmount`.

## Recording transport

Cases E2E-03 to E2E-10 and E2E-13 check which requests were sent. Put a
recording `http.RoundTripper` in `ClientConfig.HTTPClient`:

```go
type recorder struct {
	mu   sync.Mutex
	urls []*url.URL
}

func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.urls = append(r.urls, req.URL)
	r.mu.Unlock()
	return http.DefaultTransport.RoundTrip(req)
}

rec := &recorder{}
client := finder.NewClient(finder.ClientConfig{
	HTTPClient: &http.Client{Transport: rec},
})
```

The recorder also sees retries and redirects. Filter on the path
`/jobs-guest/jobs/api/seeMoreJobPostings/search` and compare distinct URLs.

## Cases

### E2E-01 Basic search returns well-formed jobs

- Input: `Query: "software engineer"`, `Locations: ["United States"]`,
  `ResultsWanted: 10`.
- Expected: `err == nil`, `len(jobs) >= 1`, and every job meets the
  invariants above.
- Gaps in `TestSearch`: it does not check `len(jobs) <= 10`, unique IDs, or
  `"N/A"` titles.

### E2E-02 `FetchDescription` fills descriptions

- Input: E2E-01 with `ResultsWanted: 3`, `FetchDescription: true`.
- Expected: `err == nil`, and every job has a non-empty `Description` in
  Markdown (no `<div` or `<p>` tags).
- Note: a single detail request that fails, for example with HTTP 429,
  makes `Search` return an error with the jobs it already has, and the test
  fails.

### E2E-03 Result cap, pagination, and unique IDs

- Input: E2E-01 with `ResultsWanted: 25`.
- Expected: `err == nil`, `len(jobs) == 25`, and IDs are unique. The first
  three search requests use `start=0`, `10`, and `20`, in that order, because
  LinkedIn returns 10 cards per page. A fourth request is allowed when pages
  repeat jobs.

### E2E-04 `Offset` starts at the requested page

- Input: E2E-01 with `Offset: 15`, `ResultsWanted: 5`.
- Expected: the first search request has `start=10`, because the offset is
  rounded down to a multiple of 10.

### E2E-05 Multiple locations are searched round-robin

- Input: `Locations: ["United States", "United Kingdom"]`,
  `ResultsWanted: 10`.
- Expected: `err == nil`, 10 unique jobs, and exactly two search requests:
  `location=United States` and then `location=United Kingdom`, both with
  `start=0`.
- Soft check (log only): results include jobs from both countries.

### E2E-06 Locations are normalized

- Input A: `Locations: [" United States ", "united states", ""]`,
  `ResultsWanted: 5`.
- Expected A: one search request with `location=United States`.
- Input B: `Locations: nil`, `ResultsWanted: 5`.
- Expected B: `err == nil`, jobs are returned, and the request has no
  `location` parameter.

### E2E-07 `HoursOld` filters by posting age

- Input: E2E-01 with `HoursOld: 24`.
- Expected: the request has `f_TPR=r86400`. Every non-nil `DatePosted` is no
  earlier than today (UTC) minus 2 days. LinkedIn dates have day precision
  and use the poster's time zone.

### E2E-08 `RemoteOnly` sends the remote filter

- Input: E2E-01 with `RemoteOnly: true`.
- Expected: the request has `f_WT=2`, `err == nil`, and at least one job is
  returned.
- Do not require `IsRemote` on every job. It is a keyword check on the title,
  location, and description, and LinkedIn's remote jobs do not always contain
  those keywords.

### E2E-09 `JobType` filters employment type

- Input: E2E-01 with `JobType: finder.JobTypeInternship`,
  `ResultsWanted: 3`, `FetchDescription: true`.
- Expected: the request has `f_JT=I`, and at least one job has
  `JobTypes == [internship]`. Log any other types without failing.
- Use internship rather than full-time: most unfiltered results are already
  full-time, so a full-time check would pass even if the filter was ignored.

### E2E-10 `CompanyIDs` filters by company

- Input: `Query: "engineer"`, `CompanyIDs: [1441]` (Google),
  `ResultsWanted: 10`.
- Expected: the request has `f_C=1441`, and every `CompanyName` contains
  `google` (case-insensitive).

### E2E-11 Description formats

Run this as a table test with `ResultsWanted: 2` and `FetchDescription: true`:

| `DescriptionFormat` | Expected `Description` |
| --- | --- |
| unset (Markdown) | Not empty, with no `<div` or `<p>` tags. |
| `DescriptionHTML` | Starts with `<div`. |
| `DescriptionPlain` | Does not start with `<div`, and equals `strings.Join(strings.Fields(d), " ")`. |

### E2E-12 Detail-page metadata

- Input: E2E-01 with `ResultsWanted: 3`, `FetchDescription: true`.
- Expected: `JobLevel` is lowercase. Each job has at least one of
  `JobLevel`, `CompanyIndustry`, or `JobFunction`. `Emails` is set only when
  `Description` contains `@`. `JobURL` does not change after enrichment.

### E2E-13 Invalid options fail without HTTP requests

Each row must return an error and `nil` jobs, and the recorder must see no
requests. These cases never reach LinkedIn, so they can also run without the
`e2e` tag.

Only full-time, part-time, contract, temporary, and internship work as search
filters. The other `JobType` constants (`summer`, `volunteer`, `perdiem`,
`nights`, `other`) only appear in detail-page results, and `Search` rejects
them.

| Options | Error contains |
| --- | --- |
| `ResultsWanted: -1` | `results wanted must not be negative` |
| `Distance: -1` | `distance must not be negative` |
| `Offset: -1` | `offset must not be negative` |
| `HoursOld: -1` | `hours old must not be negative` |
| `JobType: "freelance"` | `unsupported job type` |
| `JobType: finder.JobTypeVolunteer` | `unsupported job type` |
| `DescriptionFormat: "pdf"` | `unsupported description format` |

### E2E-14 Context cancellation stops the search

- Input: E2E-01 with a context that is already cancelled.
- Expected: `errors.Is(err, context.Canceled)`, `errors.As` gives a
  `*finder.SearchError`, no jobs are returned, and the call returns within
  1 second without retry backoff.

### E2E-15 Query with no matches

- Input: `Query: "zzqxj nonexistent role 7f3a9"`, `ResultsWanted: 10`.
- Expected: `err == nil`, the call returns, and the IDs are unique.
  `len(jobs)` may be 0. LinkedIn sometimes returns loosely related jobs, so do
  not require an empty result.

### E2E-16 Rate limiting returns `ErrRateLimited`

LinkedIn cannot be made to return HTTP 429 on request, so cover this with an
`httptest.Server` unit test instead:

- The server returns 429 and then 200. Expected: the search succeeds after
  one retry and honors `Retry-After`.
- The server always returns 429. Expected:
  `errors.Is(err, finder.ErrRateLimited)` and `StatusCode == 429` after
  4 attempts.

The client's `baseURL` and `sleep` fields are unexported, so write this as an
internal test (`package finder`) that points `baseURL` at the test server and
replaces `sleep`. This avoids the real 5 s, 10 s, and 20 s backoff.

## Findings on the current suite

1. **The suite is not compiled in CI.** `compile.yml` runs `go build ./...`,
   which skips `_test.go` files and the `e2e` tag. Adding
   `go vet -tags e2e ./...` catches compile errors without calling LinkedIn.
   Do not run the live suite in CI, because LinkedIn rate-limits and blocks
   datacenter IPs.
2. **The `Title == ""` assertion cannot fail.** See the invariants above.
3. **Network failures take about 36 seconds per test.** Transport errors,
   including a proxy that rejects the connection, are retried with a
   5 s + 10 s + 20 s backoff. When egress to `www.linkedin.com` was blocked,
   each test failed after about 36 s with `Forbidden` and no status code.
4. **Rate limiting fails the run.** The tests call `t.Fatal(err)` on any
   error. Call `t.Skip` when `errors.Is(err, finder.ErrRateLimited)` and fail
   on every other error. This keeps a LinkedIn rate limit separate from a
   parser regression.
