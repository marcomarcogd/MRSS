package dailyreport

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"
)

// Scheduler runs independently from the feed refresh schedule. It evaluates
// local wall-clock boundaries so daylight-saving changes naturally produce
// 23- or 25-hour report periods.
type Scheduler struct {
	service *Service
	store   Store
	clock   Clock

	mu     sync.Mutex
	cancel context.CancelFunc
	wakeCh chan struct{}
	loopWG sync.WaitGroup
}

func NewScheduler(service *Service, store Store, clock Clock) *Scheduler {
	if clock == nil {
		clock = RealClock()
	}
	scheduler := &Scheduler{
		service: service,
		store:   store,
		clock:   clock,
		wakeCh:  make(chan struct{}, 1),
	}
	service.SetWake(scheduler.Wake)
	return scheduler
}

// Start launches the boundary loop once. On process startup, missed periods
// are left for the explicit recovery prompt. After the loop is already alive,
// exactly one crossed boundary (for example after sleep) is generated
// automatically; multiple missed periods remain an explicit user choice.
func (s *Scheduler) Start(ctx context.Context, startup bool) {
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return
	}
	lifecycle, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.service.SetLifecycleContext(lifecycle)
	s.loopWG.Add(1)
	s.mu.Unlock()

	if err := s.MarkInterrupted(); err != nil {
		log.Printf("daily report: failed to recover interrupted runs: %v", err)
	}
	go s.loop(lifecycle, startup)
}

func (s *Scheduler) loop(ctx context.Context, startup bool) {
	defer s.loopWG.Done()
	firstEvaluation := true

	for {
		wait := 24 * time.Hour
		config, err := s.service.GetConfig()
		if err != nil {
			log.Printf("daily report: failed to load scheduler config: %v", err)
			wait = time.Minute
		} else if config.Enabled {
			missed, next, missedErr := s.service.missedPeriods(config)
			if missedErr != nil {
				log.Printf("daily report: failed to evaluate schedule: %v", missedErr)
				wait = time.Minute
			} else {
				if len(missed) == 1 && !(startup && firstEvaluation) {
					period := missed[0]
					if runErr := s.service.runScheduled(ctx, RunKindAuto, period.Start, period.End); runErr != nil &&
						!errors.Is(runErr, ErrAlreadyRunning) {
						log.Printf("daily report: failed to start scheduled run: %v", runErr)
					}
				}
				wait = next.Sub(s.clock.Now())
				if wait <= 0 {
					wait = time.Second
				}
			}
		}

		firstEvaluation = false
		timer := s.clock.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-s.wakeCh:
			timer.Stop()
		case <-timer.C():
		}
	}
}

func (s *Scheduler) Wake() {
	select {
	case s.wakeCh <- struct{}{}:
	default:
	}
}

func (s *Scheduler) Stop() {
	s.service.BeginShutdown()
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		s.loopWG.Wait()
	}
	// All report work observes the cancelled lifecycle context. Wait until each
	// job has persisted its interrupted/completed state before the owner closes
	// SQLite, so no detached goroutine can write to a closed database.
	s.service.WaitForRuns(0)
	if err := s.MarkInterrupted(); err != nil {
		log.Printf("daily report: failed to mark interrupted runs: %v", err)
	}
}

func (s *Scheduler) MarkInterrupted() error {
	return s.store.MarkRunningDailyReportsInterrupted(s.clock.Now())
}
