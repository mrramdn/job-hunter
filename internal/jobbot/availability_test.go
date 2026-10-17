package jobbot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/akmalfairuz/job-hunter/finder"
)

// fakeRepository implements the methods these tests use. Calling any other
// Repository method panics on the nil embedded interface.
type fakeRepository struct {
	Repository
	tracked   []TrackedNotification
	saved     map[int64]finder.JobStatus
	forgotten []int64
	completed map[int64]string
	content   Notification
}

func (r *fakeRepository) ListAvailabilityChecks(context.Context, time.Duration, time.Duration, int) ([]TrackedNotification, error) {
	return r.tracked, nil
}

func (r *fakeRepository) SaveAvailabilityCheck(_ context.Context, id int64, status finder.JobStatus) error {
	r.saved[id] = status
	return nil
}

func (r *fakeRepository) ForgetNotificationMessage(_ context.Context, id int64) error {
	r.forgotten = append(r.forgotten, id)
	return nil
}

func (r *fakeRepository) GetEvaluation(context.Context, Evaluation) (MatchDecision, bool, error) {
	return MatchDecision{Match: true, Overview: "Builds things."}, true, nil
}

func (r *fakeRepository) ClaimNotification(context.Context, Notification) (int64, bool, error) {
	return 7, true, nil
}

func (r *fakeRepository) WasPreviouslyNotified(context.Context, string, string, string) (bool, error) {
	return false, nil
}

func (r *fakeRepository) CompleteNotification(_ context.Context, id int64, messageID string, notification Notification) error {
	r.completed[id], r.content = messageID, notification
	return nil
}

type fakeLinkedIn struct {
	jobs     []finder.Job
	statuses map[string]finder.JobStatus
	errs     map[string]error
	checked  []string
}

func (l *fakeLinkedIn) Search(context.Context, finder.SearchOptions) ([]finder.Job, error) {
	return l.jobs, nil
}

func (l *fakeLinkedIn) JobStatus(_ context.Context, jobID string) (finder.JobStatus, error) {
	l.checked = append(l.checked, jobID)
	return l.statuses[jobID], l.errs[jobID]
}

type fakeNotifier struct {
	errs  map[string]error
	sent  []Notification
	edits map[string]Notification
}

func (n *fakeNotifier) SendJob(_ context.Context, notification Notification) (string, error) {
	n.sent = append(n.sent, notification)
	return "m-new", nil
}

func (n *fakeNotifier) EditJob(_ context.Context, messageID string, notification Notification) error {
	if err := n.errs[messageID]; err != nil {
		return err
	}
	n.edits[messageID] = notification
	return nil
}

var testAvailability = AvailabilityConfig{CheckInterval: 24 * time.Hour, MaxAge: 720 * time.Hour, BatchSize: 10}

func newTestRunner(availability AvailabilityConfig, tracked ...TrackedNotification) (*Runner, *fakeRepository, *fakeLinkedIn, *fakeNotifier) {
	repository := &fakeRepository{tracked: tracked, saved: map[int64]finder.JobStatus{}, completed: map[int64]string{}}
	linkedIn := &fakeLinkedIn{statuses: map[string]finder.JobStatus{}, errs: map[string]error{}}
	notifier := &fakeNotifier{errs: map[string]error{}, edits: map[string]Notification{}}
	scheduler := SchedulerConfig{ResultsWanted: 1, RunTimeout: time.Minute}
	runner := NewRunner(repository, linkedIn, linkedIn, nil, notifier, scheduler, availability, []string{"guild"}, slog.New(slog.DiscardHandler))
	return runner, repository, linkedIn, notifier
}

func trackedJob(id int64, jobID, messageID string) TrackedNotification {
	return TrackedNotification{ID: id, MessageID: messageID, Notification: Notification{ChannelID: "channel", LinkedInJobID: jobID, Title: "Engineer"}}
}

func TestCheckAvailabilityUpdatesMessages(t *testing.T) {
	runner, repository, linkedIn, notifier := newTestRunner(testAvailability,
		trackedJob(1, "li-1", "m1"), trackedJob(2, "li-1", "m2"), trackedJob(3, "li-2", "m3"))
	linkedIn.statuses["li-1"] = finder.JobStatusClosed
	linkedIn.statuses["li-2"] = finder.JobStatusOpen

	if err := runner.checkAvailability(context.Background()); err != nil {
		t.Fatal(err)
	}
	if want := []string{"li-1", "li-2"}; !reflect.DeepEqual(linkedIn.checked, want) {
		t.Errorf("checked jobs = %v, want %v", linkedIn.checked, want)
	}
	for messageID, want := range map[string]finder.JobStatus{"m1": finder.JobStatusClosed, "m2": finder.JobStatusClosed, "m3": finder.JobStatusOpen} {
		edit, ok := notifier.edits[messageID]
		if !ok || edit.Availability != want || edit.CheckedAt.IsZero() || edit.Title != "Engineer" {
			t.Errorf("edit of %s = %+v, want %s with a check time", messageID, edit, want)
		}
	}
	want := map[int64]finder.JobStatus{1: finder.JobStatusClosed, 2: finder.JobStatusClosed, 3: finder.JobStatusOpen}
	if !reflect.DeepEqual(repository.saved, want) {
		t.Errorf("saved = %v, want %v", repository.saved, want)
	}
}

func TestCheckAvailabilityHandlesEditFailures(t *testing.T) {
	runner, repository, linkedIn, notifier := newTestRunner(testAvailability, trackedJob(1, "li-1", "m1"), trackedJob(2, "li-2", "deleted"))
	linkedIn.statuses["li-1"] = finder.JobStatusClosed
	linkedIn.statuses["li-2"] = finder.JobStatusClosed
	notifier.errs["m1"] = errors.New("discord unavailable")
	notifier.errs["deleted"] = fmt.Errorf("%w: 404", ErrMessageNotFound)

	if err := runner.checkAvailability(context.Background()); err == nil {
		t.Fatal("expected the failed edit to be reported")
	}
	// A failed edit keeps the job open so the next check updates the message.
	if want := map[int64]finder.JobStatus{1: finder.JobStatusOpen}; !reflect.DeepEqual(repository.saved, want) {
		t.Errorf("saved = %v, want %v", repository.saved, want)
	}
	if want := []int64{2}; !reflect.DeepEqual(repository.forgotten, want) {
		t.Errorf("forgotten = %v, want %v", repository.forgotten, want)
	}
}

func TestCheckAvailabilityStopsAtFirstLinkedInError(t *testing.T) {
	runner, repository, linkedIn, notifier := newTestRunner(testAvailability, trackedJob(1, "li-1", "m1"), trackedJob(2, "li-2", "m2"))
	linkedIn.errs["li-1"] = finder.ErrRateLimited

	err := runner.checkAvailability(context.Background())
	if !errors.Is(err, finder.ErrRateLimited) {
		t.Fatalf("error = %v, want rate limit", err)
	}
	if want := []string{"li-1"}; !reflect.DeepEqual(linkedIn.checked, want) {
		t.Errorf("checked jobs = %v, want %v", linkedIn.checked, want)
	}
	if want := map[int64]finder.JobStatus{1: finder.JobStatusOpen}; !reflect.DeepEqual(repository.saved, want) {
		t.Errorf("saved = %v, want %v", repository.saved, want)
	}
	if len(notifier.edits) != 0 {
		t.Errorf("edits = %v, want none", notifier.edits)
	}
}

func TestCheckAvailabilityDisabled(t *testing.T) {
	runner, _, _, _ := newTestRunner(AvailabilityConfig{})
	runner.repository = struct{ Repository }{} // Any repository call would panic.
	if err := runner.checkAvailability(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRunStoresSentMessage(t *testing.T) {
	runner, repository, linkedIn, notifier := newTestRunner(testAvailability)
	linkedIn.jobs = []finder.Job{{ID: "li-1", Title: "Engineer", JobURL: "https://www.linkedin.com/jobs/view/1"}}
	subscription := Subscription{GuildID: "guild", ChannelID: "channel", Name: "go", Query: "go", Enabled: true}

	if err := runner.run(context.Background(), []Subscription{subscription}, 24); err != nil {
		t.Fatal(err)
	}
	if len(notifier.sent) != 1 || notifier.sent[0].Availability != finder.JobStatusOpen || notifier.sent[0].CheckedAt.IsZero() {
		t.Fatalf("sent = %+v, want one open job with a check time", notifier.sent)
	}
	if repository.completed[7] != "m-new" || repository.content.LinkedInJobID != "li-1" {
		t.Errorf("completed = %v with %+v, want message m-new for li-1", repository.completed, repository.content)
	}
}

func TestBuildJobMessageStatus(t *testing.T) {
	checkedAt := time.Unix(1700000000, 0)
	tests := []struct {
		name      string
		status    finder.JobStatus
		checkedAt time.Time
		want      string
		color     int
	}{
		{"not tracked", finder.JobStatusOpen, time.Time{}, "", 0x0A66C2},
		{"open", finder.JobStatusOpen, checkedAt, "Open, checked <t:1700000000:R>", 0x0A66C2},
		{"closed", finder.JobStatusClosed, checkedAt, "Closed <t:1700000000:R>", 0x80848E},
		{"removed", finder.JobStatusRemoved, checkedAt, "Removed <t:1700000000:R>", 0x80848E},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			embed := buildJobMessage(Notification{Title: "Engineer", Availability: test.status, CheckedAt: test.checkedAt}).Embeds[0]
			status := ""
			for _, field := range embed.Fields {
				if field.Name == "Status" {
					status = field.Value
				}
			}
			if status != test.want || embed.Color != test.color {
				t.Errorf("status = %q, color = %#x, want %q, %#x", status, embed.Color, test.want, test.color)
			}
		})
	}
}
