package recommendation

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"go-stock/backend/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	RunPending   = "pending"
	RunRunning   = "running"
	RunCompleted = "completed"
	RunFailed    = "failed"

	JobPending   = "pending"
	JobRunning   = "running"
	JobRetry     = "retry"
	JobCompleted = "completed"
	JobFailed    = "failed"
)

type AnalysisCandidate struct {
	StockCode         string
	StockName         string
	ValidationBatchID uint
	ScreeningScore    float64
	ScreeningJSON     string
}

type JobStore struct{ db *gorm.DB }

func NewJobStore(db *gorm.DB) (*JobStore, error) {
	if db == nil {
		return nil, errors.New("analysis job store requires a database")
	}
	return &JobStore{db: db}, nil
}

// CreateRun creates one durable job per candidate. The date/strategy key makes
// scheduler retries idempotent after process restarts.
func (s *JobStore) CreateRun(tradeDate time.Time, strategy string, candidates []AnalysisCandidate, maxAttempts int, now time.Time) (*models.AIAnalysisRun, bool, error) {
	if tradeDate.IsZero() || now.IsZero() || strategy == "" || len(candidates) == 0 || maxAttempts <= 0 {
		return nil, false, errors.New("trade date, strategy, candidates, attempts, and current time are required")
	}
	var run models.AIAnalysisRun
	created := false
	tradeDate = time.Date(tradeDate.Year(), tradeDate.Month(), tradeDate.Day(), 0, 0, 0, 0, tradeDate.Location())
	err := s.db.Transaction(func(tx *gorm.DB) error {
		err := tx.Where("trade_date = ? AND strategy_version = ?", tradeDate, strategy).First(&run).Error
		if err == nil {
			return verifyExistingCandidates(tx, run.ID, candidates)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := validateCandidates(tx, candidates); err != nil {
			return err
		}
		run = models.AIAnalysisRun{TradeDate: tradeDate, StrategyVersion: strategy, Status: RunPending, TotalJobs: len(candidates)}
		if err := tx.Create(&run).Error; err != nil {
			return err
		}
		jobs := make([]models.AIAnalysisJob, 0, len(candidates))
		seen := make(map[string]bool, len(candidates))
		for _, candidate := range candidates {
			code := strings.TrimSpace(candidate.StockCode)
			if seen[code] {
				return fmt.Errorf("duplicate analysis candidate %s", code)
			}
			seen[code] = true
			jobs = append(jobs, models.AIAnalysisJob{RunID: run.ID, StockCode: code, StockName: candidate.StockName,
				ValidationBatchID: candidate.ValidationBatchID, ScreeningScore: candidate.ScreeningScore, ScreeningJSON: candidate.ScreeningJSON,
				Status: JobPending, MaxAttempts: maxAttempts, AvailableAt: now})
		}
		if err := tx.Create(&jobs).Error; err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		return nil, false, fmt.Errorf("create analysis run: %w", err)
	}
	return &run, created, nil
}

func verifyExistingCandidates(tx *gorm.DB, runID uint, candidates []AnalysisCandidate) error {
	var existing []models.AIAnalysisJob
	if err := tx.Where("run_id = ?", runID).Find(&existing).Error; err != nil {
		return err
	}
	if len(existing) != len(candidates) {
		return errors.New("existing analysis run has a different candidate set")
	}
	type evidence struct {
		batch uint
		score float64
		json  string
	}
	wanted := make(map[string]evidence, len(candidates))
	for _, candidate := range candidates {
		code := strings.TrimSpace(candidate.StockCode)
		if _, duplicate := wanted[code]; code == "" || duplicate {
			return errors.New("analysis candidate set is invalid or duplicated")
		}
		wanted[code] = evidence{candidate.ValidationBatchID, candidate.ScreeningScore, candidate.ScreeningJSON}
	}
	for _, job := range existing {
		item, ok := wanted[job.StockCode]
		if !ok || item.batch != job.ValidationBatchID || item.score != job.ScreeningScore || item.json != job.ScreeningJSON {
			return errors.New("existing analysis run has different validation evidence")
		}
	}
	return nil
}

func validateCandidates(tx *gorm.DB, candidates []AnalysisCandidate) error {
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.StockCode) == "" || strings.TrimSpace(candidate.StockName) == "" || candidate.ValidationBatchID == 0 ||
			!finite(candidate.ScreeningScore) || candidate.ScreeningScore < 0 || candidate.ScreeningScore > 100 || validJSONObject(candidate.ScreeningJSON) != nil {
			return errors.New("every analysis candidate requires stock identity and validation batch")
		}
		var batch models.MarketDataValidationBatch
		if err := tx.First(&batch, candidate.ValidationBatchID).Error; err != nil {
			return fmt.Errorf("load validation batch for %s: %w", candidate.StockCode, err)
		}
		batchCode := strings.SplitN(batch.InstrumentCode, ".", 2)[0]
		candidateCode := strings.SplitN(candidate.StockCode, ".", 2)[0]
		if batch.Status != "passed" || batch.ExpectedDates <= 0 || batch.ReleasedBars != batch.ExpectedDates || batchCode != candidateCode {
			return fmt.Errorf("candidate %s does not have matching approved market data", candidate.StockCode)
		}
	}
	return nil
}

// ClaimNext leases one ready job. The conditional update prevents two workers
// from successfully claiming the same row.
func (s *JobStore) ClaimNext(now time.Time, lease time.Duration) (*models.AIAnalysisJob, error) {
	if now.IsZero() || lease <= 0 {
		return nil, errors.New("claim time and positive lease are required")
	}
	var claimed models.AIAnalysisJob
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var job models.AIAnalysisJob
		if err := tx.Where("status IN ? AND available_at <= ?", []string{JobPending, JobRetry}, now).
			Order("available_at, id").First(&job).Error; err != nil {
			return err
		}
		expires := now.Add(lease)
		result := tx.Model(&models.AIAnalysisJob{}).Where("id = ? AND status IN ?", job.ID, []string{JobPending, JobRetry}).
			Updates(map[string]any{"status": JobRunning, "attempts": gorm.Expr("attempts + 1"), "lease_expires_at": expires, "last_error": ""})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("analysis job was claimed by another worker")
		}
		if err := tx.First(&claimed, job.ID).Error; err != nil {
			return err
		}
		started := now
		return tx.Model(&models.AIAnalysisRun{}).Where("id = ? AND status = ?", job.RunID, RunPending).
			Updates(map[string]any{"status": RunRunning, "started_at": &started}).Error
	})
	if err != nil {
		return nil, err
	}
	return &claimed, nil
}

func (s *JobStore) Complete(jobID, recommendationID uint, now time.Time) error {
	if jobID == 0 || recommendationID == 0 || now.IsZero() {
		return errors.New("job, recommendation, and completion time are required")
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		return completeJob(tx, jobID, recommendationID, now)
	})
}

// CompleteWithSnapshot closes the crash window between persisting a model
// result and marking its leased job complete.
func (s *JobStore) CompleteWithSnapshot(jobID uint, snapshot *models.AIRecommendationSnapshot, now time.Time) error {
	if jobID == 0 || now.IsZero() {
		return errors.New("job and completion time are required")
	}
	if err := validateSnapshot(snapshot); err != nil {
		return err
	}
	if snapshot.SourceType != SourceAutomatic {
		return errors.New("post-close analysis job requires an automatic recommendation snapshot")
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var job models.AIAnalysisJob
		if err := tx.First(&job, jobID).Error; err != nil {
			return err
		}
		if job.Status != JobRunning {
			return errors.New("only a running analysis job can persist a result")
		}
		if snapshot.ValidationBatchID != job.ValidationBatchID || strings.SplitN(snapshot.StockCode, ".", 2)[0] != strings.SplitN(job.StockCode, ".", 2)[0] {
			return errors.New("analysis result does not match leased job evidence")
		}
		if err := requireValidationBatch(tx, snapshot); err != nil {
			return err
		}
		if err := tx.Create(snapshot).Error; err != nil {
			return fmt.Errorf("create recommendation snapshot: %w", err)
		}
		review := models.AIRecommendationReview{RecommendationID: snapshot.ID, OriginalDueDate: snapshot.ReviewDueDate, Status: StatusPending}
		if err := tx.Create(&review).Error; err != nil {
			return fmt.Errorf("create pending recommendation review: %w", err)
		}
		return completeJob(tx, jobID, snapshot.ID, now)
	})
}

func completeJob(tx *gorm.DB, jobID, recommendationID uint, now time.Time) error {
	var job models.AIAnalysisJob
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&job, jobID).Error; err != nil {
		return err
	}
	if job.Status == JobCompleted {
		if job.RecommendationID != nil && *job.RecommendationID == recommendationID {
			return nil
		}
		return errors.New("analysis job already completed with another recommendation")
	}
	if job.Status != JobRunning {
		return errors.New("only a running analysis job can complete")
	}
	var snapshot models.AIRecommendationSnapshot
	if err := tx.First(&snapshot, recommendationID).Error; err != nil {
		return err
	}
	if snapshot.ValidationBatchID != job.ValidationBatchID || strings.SplitN(snapshot.StockCode, ".", 2)[0] != strings.SplitN(job.StockCode, ".", 2)[0] {
		return errors.New("recommendation does not belong to analysis job evidence")
	}
	if err := tx.Model(&job).Updates(map[string]any{"status": JobCompleted, "recommendation_id": recommendationID, "lease_expires_at": nil}).Error; err != nil {
		return err
	}
	return refreshRun(tx, job.RunID, now)
}

func (s *JobStore) Fail(jobID uint, message string, retryAt, now time.Time) error {
	if jobID == 0 || strings.TrimSpace(message) == "" || now.IsZero() {
		return errors.New("job, failure message, and current time are required")
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var job models.AIAnalysisJob
		if err := tx.First(&job, jobID).Error; err != nil {
			return err
		}
		if job.Status != JobRunning {
			return errors.New("only a running analysis job can fail")
		}
		status := JobFailed
		available := now
		if job.Attempts < job.MaxAttempts {
			if retryAt.Before(now) {
				return errors.New("retry time cannot precede failure time")
			}
			status, available = JobRetry, retryAt
		}
		if err := tx.Model(&job).Updates(map[string]any{"status": status, "available_at": available,
			"lease_expires_at": nil, "last_error": strings.TrimSpace(message)}).Error; err != nil {
			return err
		}
		return refreshRun(tx, job.RunID, now)
	})
}

// Defer returns a claimed job without consuming an attempt when no external
// model request was made (for example, the monthly hard budget blocked it).
func (s *JobStore) Defer(jobID uint, reason string, availableAt, now time.Time) error {
	if jobID == 0 || strings.TrimSpace(reason) == "" || now.IsZero() || availableAt.Before(now) {
		return errors.New("job, defer reason, current time, and future availability are required")
	}
	result := s.db.Model(&models.AIAnalysisJob{}).Where("id = ? AND status = ? AND attempts > 0", jobID, JobRunning).
		Updates(map[string]any{"status": JobRetry, "attempts": gorm.Expr("attempts - 1"), "available_at": availableAt,
			"lease_expires_at": nil, "last_error": strings.TrimSpace(reason)})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("only a claimed analysis job can be deferred")
	}
	return nil
}

// RecoverExpired returns abandoned leases to the retry queue or exhausts them.
func (s *JobStore) RecoverExpired(now time.Time) (int64, error) {
	if now.IsZero() {
		return 0, errors.New("recovery time is required")
	}
	var affected int64
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var expired []models.AIAnalysisJob
		if err := tx.Where("status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at <= ?", JobRunning, now).Find(&expired).Error; err != nil {
			return err
		}
		runs := make(map[uint]bool)
		for _, job := range expired {
			status, message := JobRetry, "worker lease expired"
			if job.Attempts >= job.MaxAttempts {
				status, message = JobFailed, "worker lease expired after final attempt"
			}
			result := tx.Model(&models.AIAnalysisJob{}).Where("id = ? AND status = ?", job.ID, JobRunning).
				Updates(map[string]any{"status": status, "available_at": now, "lease_expires_at": nil, "last_error": message})
			if result.Error != nil {
				return result.Error
			}
			affected += result.RowsAffected
			runs[job.RunID] = true
		}
		for runID := range runs {
			if err := refreshRun(tx, runID, now); err != nil {
				return err
			}
		}
		return nil
	})
	return affected, err
}

func refreshRun(tx *gorm.DB, runID uint, now time.Time) error {
	var completed, failed int64
	if err := tx.Model(&models.AIAnalysisJob{}).Where("run_id = ? AND status = ?", runID, JobCompleted).Count(&completed).Error; err != nil {
		return err
	}
	if err := tx.Model(&models.AIAnalysisJob{}).Where("run_id = ? AND status = ?", runID, JobFailed).Count(&failed).Error; err != nil {
		return err
	}
	var run models.AIAnalysisRun
	if err := tx.First(&run, runID).Error; err != nil {
		return err
	}
	updates := map[string]any{"completed_jobs": completed, "failed_jobs": failed}
	if int(completed+failed) == run.TotalJobs {
		status := RunCompleted
		if failed > 0 {
			status = RunFailed
		}
		updates["status"], updates["completed_at"] = status, &now
	}
	return tx.Model(&run).Updates(updates).Error
}
