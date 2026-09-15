package serval

// Ordering two values the same way in two places at once.
//
// A sequence's records come from more than one place -- some held here, some
// known only to something else that has to be asked -- and each place orders
// what it holds before the answers are folded together. That only works if both
// compute the SAME order from the same spec without conferring, so the rules
// are exact, small enough to implement twice, and free of anything whose answer
// depends on the machine or the locale.
//
// docs/ordering.md is the spec this implements.

import (
	"math"
	"strings"
)

// The collations text may be compared under. A symbol and a run of bytes take
// none: a symbol is a name, and bytes are not text.
const (
	CollateExact   = "exact"   // codepoint order, rune by rune
	CollateFold    = "fold"    // ASCII case folded, then exact
	CollateNatural = "natural" // digit runs as numbers, then fold
)

// A value's rank, which its kind decides before anything in it is read.
const (
	RankUndefined = iota
	RankNil
	RankFalse
	RankTrue
	RankNumber // int and float share one rank and interleave by value
	RankSymbol
	RankString
	RankBytes
	RankUnordered // no order of its own; the sort's last level settles it
)

// Rank is where a value sits before its contents matter. A value that is not
// there at all is undefined, which is the bottom of the order.
//
// The kind decides it outright. Nothing is read out of the value and nothing is
// spelled: which rank a value has is a fact about what it IS.
func Rank(v *Value) int {
	if v == nil {
		return RankUndefined
	}
	switch v.Kind {
	case NilValue:
		return RankNil
	case BoolValue:
		if v.Bool {
			return RankTrue
		}
		return RankFalse
	case NumberValue:
		return RankNumber
	case SymbolValue:
		return RankSymbol
	case TextValue:
		return RankString
	case BytesValue:
		return RankBytes
	}
	return RankUnordered // a list has no order of its own
}

// Compare orders two values: -1, 0 or 1. The collation applies to the string
// rank alone and is CollateExact when empty.
//
// Two values of different ranks are decided by the ranks. Two of the same rank
// are decided by what is in them, except the ranks that hold one value each
// (undefined, nil, false, true) and the unordered rank, which compare equal and
// leave the answer to the sort's last level.
func Compare(a, b *Value, collation string) int {
	ra, rb := Rank(a), Rank(b)
	if ra != rb {
		return sign(ra - rb)
	}
	switch ra {
	case RankNumber:
		return compareNumbers(a, b)
	case RankSymbol:
		return compareRunes(a.Str, b.Str)
	case RankString:
		return collate(a.Str, b.Str, collation)
	case RankBytes:
		return strings.Compare(a.Str, b.Str) // the bytes as they stand
	}
	return 0
}

// Level is one level of a sort, as it applies to a pair of values already
// taken from the two records being ordered.
type Level struct {
	Descending bool
	Collation  string
}

// CompareLevels walks the levels in order and answers on the first that
// separates the two runs, so each level settles only what the ones above it
// left equal. A run shorter than the levels stops where it ends.
//
// The caller appends the record's identity as a final level: two records equal on
// every level a sort names must still have an order, or "the record after this
// point" names more than one place.
func CompareLevels(a, b []*Value, levels []Level) int {
	for i, level := range levels {
		if i >= len(a) || i >= len(b) {
			break
		}
		c := Compare(a[i], b[i], level.Collation)
		if c == 0 {
			continue
		}
		if level.Descending {
			return -c
		}
		return c
	}
	return 0
}

// collate compares two strings under one collation.
func collate(a, b, collation string) int {
	switch collation {
	case CollateFold:
		return compareFolded(a, b)
	case CollateNatural:
		return compareNatural(a, b)
	}
	return compareRunes(a, b)
}

// compareRunes is codepoint order, one rune against one. For well-formed UTF-8
// this is the byte order too; it is written as runes because that is what the
// rule says and what a malformed encoding would otherwise get wrong.
func compareRunes(a, b string) int {
	ar, br := []rune(a), []rune(b)
	for i := 0; i < len(ar) && i < len(br); i++ {
		if ar[i] != br[i] {
			return sign(int(ar[i]) - int(br[i]))
		}
	}
	return sign(len(ar) - len(br))
}

// compareFolded is compareRunes with ASCII case folded away. Nothing else is
// touched: a fold that reached further would be one two implementations could
// disagree about.
func compareFolded(a, b string) int {
	ar, br := []rune(a), []rune(b)
	for i := 0; i < len(ar) && i < len(br); i++ {
		x, y := foldRune(ar[i]), foldRune(br[i])
		if x != y {
			return sign(int(x) - int(y))
		}
	}
	return sign(len(ar) - len(br))
}

// compareNatural reads a run of ASCII digits as a number and everything else
// as folded runes, so file9 comes before file10.
func compareNatural(a, b string) int {
	ar, br := []rune(a), []rune(b)
	i, j := 0, 0
	for i < len(ar) && j < len(br) {
		if isDigit(ar[i]) && isDigit(br[j]) {
			startA, startB := i, j
			for i < len(ar) && isDigit(ar[i]) {
				i++
			}
			for j < len(br) && isDigit(br[j]) {
				j++
			}
			if c := compareDigitRuns(ar[startA:i], br[startB:j]); c != 0 {
				return c
			}
			continue
		}
		x, y := foldRune(ar[i]), foldRune(br[j])
		if x != y {
			return sign(int(x) - int(y))
		}
		i++
		j++
	}
	return sign((len(ar) - i) - (len(br) - j))
}

// compareDigitRuns compares two runs of digits as numbers, however long they
// are: with the leading zeros gone, the longer run is the larger number and
// equal lengths compare digit by digit. Two runs of the same value are then
// separated by the zeros themselves, so 01 and 1 are not the same string.
func compareDigitRuns(a, b []rune) int {
	as, bs := withoutLeadingZeros(a), withoutLeadingZeros(b)
	if len(as) != len(bs) {
		return sign(len(as) - len(bs))
	}
	for i := range as {
		if as[i] != bs[i] {
			return sign(int(as[i]) - int(bs[i]))
		}
	}
	return sign(len(a) - len(b))
}

func withoutLeadingZeros(r []rune) []rune {
	i := 0
	for i < len(r)-1 && r[i] == '0' {
		i++
	}
	return r[i:]
}

// compareNumbers orders two numbers exactly. Two integers compare as integers
// however large they are, and an integer against a float compares without
// either being converted: casting the integer loses digits, and casting the
// float loses the fraction that decides it.
func compareNumbers(a, b *Value) int {
	switch {
	case a.IsInt && b.IsInt:
		return signOf(a.Int, b.Int)
	case a.IsInt:
		return -compareFloatToInt(b.Num, a.Int)
	case b.IsInt:
		return compareFloatToInt(a.Num, b.Int)
	}
	switch {
	case a.Num < b.Num:
		return -1
	case a.Num > b.Num:
		return 1
	}
	return 0
}

// compareFloatToInt orders a float against an integer: -1 if the float is the
// smaller. A float too large to be an int64 is decided by its sign alone;
// otherwise the whole part decides and the fraction breaks the tie.
func compareFloatToInt(f float64, i int64) int {
	if math.IsNaN(f) {
		return 0
	}
	const (
		tooLarge = 9223372036854775808.0  // 2^63, one past the largest int64
		tooSmall = -9223372036854775808.0 // -2^63, the smallest int64 exactly
	)
	if f >= tooLarge {
		return 1
	}
	if f < tooSmall {
		return -1
	}
	whole := int64(math.Trunc(f))
	if whole != i {
		return signOf(whole, i)
	}
	switch frac := f - math.Trunc(f); {
	case frac > 0:
		return 1
	case frac < 0:
		return -1
	}
	return 0
}

func foldRune(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + ('a' - 'A')
	}
	return r
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

// signOf orders two integers without subtracting them, a difference of two
// int64s being able to overflow one.
func signOf(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
