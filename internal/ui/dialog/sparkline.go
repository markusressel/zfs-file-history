package dialog

// sparklineRunes are the bar heights of a sparkline, lowest first.
var sparklineRunes = []rune("▁▂▃▄▅▆▇█")

// sparklineGap marks values that do not exist (e.g. the folder is missing in a snapshot).
const sparklineGap = ' '

// sparkline renders values as a small bar chart of at most width runes. Negative values are gaps. If there are more
// values than columns, each column shows the maximum of the values it covers. column returns the column of the value
// with the given index.
func sparkline(values []int64, width int) (line []rune, column func(index int) int) {
	if width <= 0 || len(values) == 0 {
		return nil, func(int) int { return 0 }
	}
	columns := min(len(values), width)
	column = func(index int) int {
		return min(index*columns/len(values), columns-1)
	}

	buckets := make([]int64, columns)
	for i := range buckets {
		buckets[i] = -1
	}
	var maxValue int64
	for index, value := range values {
		bucket := column(index)
		buckets[bucket] = max(buckets[bucket], value)
		maxValue = max(maxValue, value)
	}

	line = make([]rune, columns)
	for i, value := range buckets {
		switch {
		case value < 0:
			line[i] = sparklineGap
		case maxValue == 0:
			line[i] = sparklineRunes[0]
		default:
			level := int(value * int64(len(sparklineRunes)-1) / maxValue)
			line[i] = sparklineRunes[level]
		}
	}
	return line, column
}
