package jobbot

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/akmalfairuz/job-hunter/finder"
)

// checkAvailability re-checks sent jobs on LinkedIn and re-renders their
// Discord messages with the result. Each job is requested once per call, and
// the call stops at the first failed request because LinkedIn may be limiting
// requests.
func (runner *Runner) checkAvailability(ctx context.Context) error {
	config := runner.availability
	if !config.Enabled() {
		return nil
	}
	tracked, err := runner.repository.ListAvailabilityChecks(ctx, config.CheckInterval, config.MaxAge, config.BatchSize)
	if err != nil {
		return err
	}
	statuses := make(map[string]finder.JobStatus, len(tracked))
	var checkErrors []error
	for _, item := range tracked {
		jobID := item.Notification.LinkedInJobID
		status, checked := statuses[jobID]
		if !checked {
			status, err = runner.checker.JobStatus(ctx, jobID)
			if err != nil {
				checkErrors = append(checkErrors, fmt.Errorf("check availability of %s: %w", jobID, err))
				// Recording the attempt moves this job behind the others, so a
				// job whose page keeps failing cannot block the queue.
				if ctx.Err() == nil {
					if err := runner.repository.SaveAvailabilityCheck(ctx, item.ID, finder.JobStatusOpen); err != nil {
						checkErrors = append(checkErrors, err)
					}
				}
				break
			}
			statuses[jobID] = status
			if status != finder.JobStatusOpen {
				runner.logger.Info("job is no longer available", "job_id", jobID, "status", status)
			}
		}

		notification := item.Notification
		notification.Availability, notification.CheckedAt = status, time.Now()
		saved := status
		if err := runner.notifier.EditJob(ctx, item.MessageID, notification); err != nil {
			if errors.Is(err, ErrMessageNotFound) {
				if err := runner.repository.ForgetNotificationMessage(ctx, item.ID); err != nil {
					checkErrors = append(checkErrors, err)
				}
				continue
			}
			checkErrors = append(checkErrors, fmt.Errorf("update message for %s: %w", jobID, err))
			// Leave the job open so the message is updated after the next check.
			saved = finder.JobStatusOpen
		}
		if err := runner.repository.SaveAvailabilityCheck(ctx, item.ID, saved); err != nil {
			checkErrors = append(checkErrors, err)
		}
	}
	return errors.Join(checkErrors...)
}
