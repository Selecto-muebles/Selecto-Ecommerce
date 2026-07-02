package collection

import "sort"

func Map[T any, R any](items []T, transform func(T) R) []R {
	result := make([]R, 0, len(items))
	for _, item := range items {
		result = append(result, transform(item))
	}
	return result
}

func Filter[T any](items []T, keep func(T) bool) []T {
	result := make([]T, 0, len(items))
	for _, item := range items {
		if keep(item) {
			result = append(result, item)
		}
	}
	return result
}

func Reduce[T any, R any](items []T, initial R, reducer func(R, T) R) R {
	result := initial
	for _, item := range items {
		result = reducer(result, item)
	}
	return result
}

func Find[T any](items []T, matches func(T) bool) (T, bool) {
	for _, item := range items {
		if matches(item) {
			return item, true
		}
	}

	var zero T
	return zero, false
}

func Contains[T any](items []T, matches func(T) bool) bool {
	_, ok := Find(items, matches)
	return ok
}

func GroupSumByInt[T any](items []T, key func(T) int, value func(T) int) map[int]int {
	result := make(map[int]int)
	for _, item := range items {
		result[key(item)] += value(item)
	}
	return result
}

func SortedIntKeys[V any](items map[int]V) []int {
	keys := make([]int, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Ints(keys)
	return keys
}
