package parser

import (
	"errors"
	"fmt"
)

// Limits controls resource usage during OpenAPI parsing and conversion.
// A zero value means "use the defaults"; callers should call DefaultLimits
// to obtain a populated value.
type Limits struct {
	// MaxDepth is the maximum recursion depth allowed when converting nested
	// schemas on the OpenAPI 3.0.x and 3.1.x paths (v30.go/v31.go), where it is
	// enforced by entering/leaving a budget frame per schema. Depth is measured
	// as the number of nested schema/reference frames entered. A value of zero
	// disables the converter depth limit.
	//
	// The Swagger 2.0 path (ConvertV2) does not enter per-frame budget calls,
	// so MaxDepth is not enforced there; v2 relies on the lexer's structural
	// nesting cap (maxNestingDepth) to bound recursion. Lowering MaxDepth below
	// maxNestingDepth therefore affects only 3.x conversion (M-29).
	MaxDepth int

	// MaxMemoryBytes is an approximate memory budget in bytes for the parser.
	// It is enforced by estimating the cumulative heap occupied by AST nodes
	// (per-node overhead plus retained raw source text) before conversion, and
	// by per-frame accounting during 3.x schema conversion. The estimate is
	// coarse; treat the limit as a guardrail against pathological input rather
	// than a precise cap. A value of zero disables the memory budget (M-30).
	MaxMemoryBytes int64
}

// DefaultLimits returns sensible parser guardrails. The defaults are large
// enough for normal OpenAPI specs but prevent runaway recursion or OOM on
// pathological input.
func DefaultLimits() Limits {
	return Limits{
		MaxDepth:       1000,
		MaxMemoryBytes: 256 << 20, // 256 MiB
	}
}

// ErrDepthLimitExceeded is returned when the parser exceeds MaxDepth.
var ErrDepthLimitExceeded = errors.New("recursion depth limit exceeded")

// ErrMemoryBudgetExceeded is returned when the parser exceeds MaxMemoryBytes.
var ErrMemoryBudgetExceeded = errors.New("memory budget exceeded")

// Budget tracks resource consumption during a single parse/conversion.
// It is safe to use from a single goroutine only.
type Budget struct {
	Limits
	depth int
	bytes int64
}

// NewBudget creates a fresh budget from the supplied limits.
func NewBudget(l Limits) *Budget {
	return &Budget{Limits: l}
}

// Enter records entry into a recursive frame. If the frame would exceed either
// the depth limit or the memory budget, Enter returns an error and the caller
// must not process the frame.
func (b *Budget) Enter(cost int64) error {
	if b == nil {
		return nil
	}
	b.depth++
	if b.MaxDepth > 0 && b.depth > b.MaxDepth {
		b.depth--
		return fmt.Errorf("%w (depth %d)", ErrDepthLimitExceeded, b.depth)
	}
	if b.MaxMemoryBytes > 0 && b.bytes+cost > b.MaxMemoryBytes {
		b.depth--
		return fmt.Errorf("%w (%d bytes)", ErrMemoryBudgetExceeded, b.bytes+cost)
	}
	b.bytes += cost
	return nil
}

// Leave records exit from a recursive frame. It must be paired with a
// successful Enter.
func (b *Budget) Leave() {
	if b == nil {
		return
	}
	if b.depth > 0 {
		b.depth--
	}
}

// Account adds non-recursive bytes to the budget. It returns an error if the
// budget is exceeded.
func (b *Budget) Account(cost int64) error {
	if b == nil {
		return nil
	}
	if b.MaxMemoryBytes > 0 && b.bytes+cost > b.MaxMemoryBytes {
		return fmt.Errorf("%w (%d bytes)", ErrMemoryBudgetExceeded, b.bytes+cost)
	}
	b.bytes += cost
	return nil
}

// ConvertOption configures a ConvertV30 / ConvertV31 / ConvertV2 call.
type ConvertOption func(*convertConfig)

type convertConfig struct {
	limits Limits
}

// WithLimits sets the parser resource limits for a conversion.
func WithLimits(l Limits) ConvertOption {
	return func(c *convertConfig) {
		c.limits = l
	}
}

func defaultConvertConfig() *convertConfig {
	return &convertConfig{limits: DefaultLimits()}
}

// budgetExceededDiag returns a diagnostic used when a memory or depth limit is
// exceeded while scanning an OpenAPI document.
func budgetExceededDiag(err error, loc SourceLocation) Diagnostic {
	return Diagnostic{
		Severity:       SeverityError,
		Summary:        "Resource limit reached during schema scan",
		Detail:         err.Error(),
		SourceLocation: &loc,
	}
}

// nodeOverheadBytes is the approximate heap overhead per AST node: the struct
// header, interface/pointer fields, and the SourceLocation. It converts the
// node count produced by estimateNodeMemory into a byte figure comparable to
// Limits.MaxMemoryBytes, so the memory budget can actually fire before real
// memory is exhausted (M-30).
const nodeOverheadBytes int64 = 96

// estimateNodeMemory returns a rough byte estimate for the heap occupied by the
// AST rooted at n. It sums a per-node overhead plus the raw source text retained
// by scalar nodes, and is used as a coarse memory-budget estimate before
// beginning conversion. The recursion is depth-bounded by maxNestingDepth so a
// pathological (or hand-crafted) tree cannot overflow the estimator itself
// (M-30).
func estimateNodeMemory(n Node) int64 {
	var total int64
	estimateNodeMemoryAt(n, 0, &total)
	return total
}

func estimateNodeMemoryAt(n Node, depth int, total *int64) {
	if n == nil || depth > maxNestingDepth {
		return
	}
	*total += nodeOverheadBytes
	switch v := n.(type) {
	case *MapNode:
		// Account for the entries slice backing array.
		*total += int64(len(v.Entries)) * 16
		for _, e := range v.Entries {
			estimateNodeMemoryAt(e.Key, depth+1, total)
			estimateNodeMemoryAt(e.Value, depth+1, total)
		}
	case *SequenceNode:
		*total += int64(len(v.Items)) * 8
		for _, item := range v.Items {
			estimateNodeMemoryAt(item, depth+1, total)
		}
	case *ScalarNode:
		// Scalar nodes retain their raw source text; that string is usually
		// the dominant per-node cost for string-heavy specs.
		*total += int64(len(v.Raw))
	}
}
