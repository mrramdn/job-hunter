# Finder end-to-end test cases

Run the automated cases with `go test -tags e2e ./finder`. They call LinkedIn
directly, so they check invariants instead of specific jobs.

Every returned job must have an `ID` that matches `^li-[0-9]+$` and is unique in
the result, and a `JobURL` of `https://www.linkedin.com/jobs/view/<id>`. `Title`
and `CompanyName` must not be `"N/A"`, which the parser uses when a selector
finds nothing. `len(jobs)` must not exceed `ResultsWanted`.

Cases that check request parameters record the outgoing URLs with an
`http.RoundTripper` passed in `ClientConfig.HTTPClient`.

| ID | Case | Input | Expected | Status |
| --- | --- | --- | --- | --- |
| E2E-01 | Basic search | `Query: "software engineer"`, `Locations: ["United States"]`, `ResultsWanted: 10` | No error and at least one job | Automated (`TestSearch`) |
| E2E-02 | Descriptions | E2E-01 with `ResultsWanted: 3`, `FetchDescription: true` | Every job has a non-empty Markdown `Description` | Automated (`TestSearch_FetchesDescription`) |
| E2E-03 | Pagination | E2E-01 with `ResultsWanted: 25` | 25 unique jobs; the first requests use `start=0`, `10`, `20` | Proposed |
| E2E-04 | Offset | E2E-01 with `Offset: 15`, `ResultsWanted: 5` | The first request uses `start=10` | Proposed |
| E2E-05 | Multiple locations | `Locations: ["United States", "United Kingdom"]`, `ResultsWanted: 10` | 10 unique jobs from two requests, US then UK, both with `start=0` | Proposed |
| E2E-06 | Location normalization | `Locations: [" United States ", "united states", ""]`, `ResultsWanted: 5` | One request with `location=United States` | Proposed |
| E2E-07 | Posting age | E2E-01 with `HoursOld: 24` | The request has `f_TPR=r86400`; every `DatePosted` is at most 2 days old | Proposed |
| E2E-08 | Remote only | E2E-01 with `RemoteOnly: true` | The request has `f_WT=2`; at least one job | Proposed |
| E2E-09 | Job type | E2E-01 with `JobType: internship`, `ResultsWanted: 3`, `FetchDescription: true` | The request has `f_JT=I`; at least one job has `JobTypes == [internship]` | Proposed |
| E2E-10 | Company | `Query: "engineer"`, `CompanyIDs: [1441]` (Google) | The request has `f_C=1441`; every `CompanyName` contains `Google` | Proposed |
| E2E-11 | Description formats | E2E-02 with each `DescriptionFormat` | Markdown has no `<div`; HTML starts with `<div`; plain text has no repeated whitespace | Proposed |
| E2E-12 | Detail metadata | E2E-02 | `JobLevel` is lowercase; `Emails` is set only when `Description` contains `@` | Proposed |
| E2E-13 | Invalid options | A negative `ResultsWanted`, `Distance`, `Offset`, or `HoursOld`; `JobType: "freelance"` or `JobTypeVolunteer` (not a search filter); `DescriptionFormat: "pdf"` | An error, `nil` jobs, and no HTTP requests | Proposed |
| E2E-14 | Cancelled context | E2E-01 with a cancelled context | `errors.Is(err, context.Canceled)`; returns within 1 second | Proposed |
| E2E-15 | No matches | `Query: "zzqxj nonexistent role 7f3a9"` | No error; `len(jobs)` may be 0 | Proposed |
| E2E-16 | Rate limit | A test server that always returns HTTP 429 | `errors.Is(err, finder.ErrRateLimited)` after 4 attempts | Unit test (`httptest.Server`) |
