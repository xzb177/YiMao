// Package main provides utility functions for the Emby Telegram bot
package main

import (
	"math"
)

// Min returns the minimum of two integers
func Min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Max returns the maximum of two integers
func Max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// MinInt64 returns the minimum of two int64 values
func MinInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// MaxInt64 returns the maximum of two int64 values
func MaxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// Clamp clamps a value between min and max
func Clamp(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// SafeStringSlice safely slices a string to avoid panics
func SafeStringSlice(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// SafeTruncate truncates a string to max length and adds ellipsis if needed
func SafeTruncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// Abs returns the absolute value of an integer
func Abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// Round rounds a float64 to the nearest integer
func Round(x float64) int {
	return int(math.Floor(x + 0.5))
}

// DivCeil performs ceiling division
func DivCeil(a, b int) int {
	return (a + b - 1) / b
}
