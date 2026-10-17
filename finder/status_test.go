package finder

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	openJobPage = `<section class="top-card-layout"><h2 class="top-card-layout__title">Engineer</h2>
		<figure class="num-applicants__figure"><figcaption>Over 200 applicants</figcaption></figure>
		<button class="sign-up-modal__outlet">Apply</button></section>
		<div class="show-more-less-html__markup">We are no longer accepting applications after June.</div>`
	closedJobPage = `<section class="top-card-layout"><h2 class="top-card-layout__title">Engineer</h2></section>
		<div class="sticky-header"><figure class="closed-job"><figcaption class="closed-job__flavor--closed">
		No longer accepting applications</figcaption></figure></div>`
	closedTextJobPage = `<section class="top-card-layout"><h2 class="top-card-layout__title">Engineer</h2>
		<span>No longer accepting applications</span></section>`
	closedCaptionJobPage = `<section class="job-header"><h2>Engineer</h2>
		<figure class="job-state"><figcaption>No longer accepting applications</figcaption></figure></section>`
)

func TestParseJobStatus(t *testing.T) {
	tests := []struct {
		name string
		page string
		want JobStatus
	}{
		{"open with phrase in description", openJobPage, JobStatusOpen},
		{"closed notice", closedJobPage, JobStatusClosed},
		{"closed text in top card", closedTextJobPage, JobStatusClosed},
		{"closed text in renamed markup", closedCaptionJobPage, JobStatusClosed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseJobStatus(strings.NewReader(test.page))
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("status = %q, want %q", got, test.want)
			}
		})
	}
}

func TestClientJobStatus(t *testing.T) {
	mux := http.NewServeMux()
	page := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }
	}
	mux.HandleFunc("/jobs/view/1", page(openJobPage))
	mux.HandleFunc("/jobs/view/2", page(closedJobPage))
	mux.HandleFunc("/jobs/view/3", http.NotFound)
	mux.HandleFunc("/jobs/view/4", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/jobs/search?trk=expired_jd_redirect", http.StatusFound)
	})
	mux.HandleFunc("/jobs/search", page("<html></html>"))
	mux.HandleFunc("/jobs/view/5", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/authwall?trk=job", http.StatusFound)
	})
	mux.HandleFunc("/authwall", page("<html></html>"))
	mux.HandleFunc("/jobs/view/6", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/jobs/search?currentJobId=6", http.StatusFound)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewClient(ClientConfig{HTTPClient: server.Client()})
	client.baseURL = server.URL
	tests := []struct {
		id      string
		want    JobStatus
		wantErr bool
	}{
		{id: "li-1", want: JobStatusOpen},
		{id: "2", want: JobStatusClosed},
		{id: "li-3", want: JobStatusRemoved},
		{id: "li-4", want: JobStatusRemoved},
		{id: "li-5", wantErr: true},
		{id: "li-6", wantErr: true},
		{id: "li-", wantErr: true},
	}
	for _, test := range tests {
		got, err := client.JobStatus(context.Background(), test.id)
		if test.wantErr {
			if err == nil {
				t.Errorf("JobStatus(%q) = %q, want error", test.id, got)
			}
			continue
		}
		if err != nil || got != test.want {
			t.Errorf("JobStatus(%q) = %q, %v, want %q", test.id, got, err, test.want)
		}
	}
}
