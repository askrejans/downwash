package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// AnalysisLimits bounds retained frames and bytes of metadata, excluding video media.
// A zero field leaves that resource unrestricted.
type AnalysisLimits struct {
	// MaxFrames also bounds individual metadata containers to MaxFrames+64
	// entries and limits structured metadata nesting to 64 levels.
	MaxFrames        int
	MaxMetadataBytes int64
}

var ErrAnalysisLimit = errors.New("telemetry: analysis resource limit exceeded")

type analysisBudget struct {
	limits AnalysisLimits
	bytes  int64
}
type analysisBudgetKey struct{}

func WithAnalysisLimits(ctx context.Context, limits AnalysisLimits) context.Context {
	return context.WithValue(ctx, analysisBudgetKey{}, &analysisBudget{limits: limits})
}

func budget(ctx context.Context) *analysisBudget {
	b, _ := ctx.Value(analysisBudgetKey{}).(*analysisBudget)
	return b
}

// MetadataAllocation checks before allocating and counts all extracted metadata bytes.
func MetadataAllocation(ctx context.Context, size int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b := budget(ctx); b != nil && b.limits.MaxMetadataBytes > 0 {
		if size < 0 || size > b.limits.MaxMetadataBytes-b.bytes {
			return fmt.Errorf("%w: metadata bytes", ErrAnalysisLimit)
		}
		b.bytes += size
	}
	return nil
}

func CheckFrameCount(ctx context.Context, count int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b := budget(ctx); b != nil && b.limits.MaxFrames > 0 && count > b.limits.MaxFrames {
		return fmt.Errorf("%w: frame count", ErrAnalysisLimit)
	}
	return nil
}

// Table entries are bounded before slices/maps grow, including non-frame headers.
func checkTableCount(ctx context.Context, count uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b := budget(ctx); b != nil && b.limits.MaxFrames > 0 && count > uint64(b.limits.MaxFrames)+64 {
		return fmt.Errorf("%w: metadata table entries", ErrAnalysisLimit)
	}
	return nil
}

func CheckMetadataSize(ctx context.Context, size int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b := budget(ctx); b != nil && b.limits.MaxMetadataBytes > 0 && size > b.limits.MaxMetadataBytes-b.bytes {
		return fmt.Errorf("%w: metadata bytes", ErrAnalysisLimit)
	}
	return nil
}

type analysisReader struct {
	ctx    context.Context
	source io.Reader
}

func AnalysisReader(ctx context.Context, source io.Reader) io.Reader {
	return &analysisReader{ctx, source}
}

// ValidateMetadataJSON scans bounded input before decoding maps or frame slices.
// Its independent byte counter does not count the same source twice.
func ValidateMetadataJSON(ctx context.Context, source io.Reader) error {
	b := budget(ctx)
	if b == nil || b.limits.MaxFrames == 0 {
		return nil
	}
	ctx = WithAnalysisLimits(ctx, b.limits)
	decoder := json.NewDecoder(AnalysisReader(ctx, source))
	var scan func(int) error
	scan = func(depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > 64 {
			return fmt.Errorf("%w: metadata nesting", ErrAnalysisLimit)
		}
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		if delimiter, ok := token.(json.Delim); ok {
			if delimiter != '{' && delimiter != '[' {
				return fmt.Errorf("invalid metadata JSON delimiter")
			}
			for count := uint64(1); decoder.More(); count++ {
				if err := checkTableCount(ctx, count); err != nil {
					return err
				}
				if delimiter == '{' {
					if _, err := decoder.Token(); err != nil {
						return err
					}
				}
				if err := scan(depth + 1); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
		}
		return err
	}
	return scan(0)
}

func (r *analysisReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if b := budget(r.ctx); b != nil && b.limits.MaxMetadataBytes > 0 {
		remaining := b.limits.MaxMetadataBytes - b.bytes
		if remaining < int64(len(data)) && remaining+1 < int64(len(data)) {
			data = data[:remaining+1]
		}
	}
	n, err := r.source.Read(data)
	if limitErr := MetadataAllocation(r.ctx, int64(n)); limitErr != nil {
		return 0, limitErr
	}
	return n, err
}
