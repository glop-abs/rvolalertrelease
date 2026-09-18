package main

import "sort"

// AddOrUpdate 添加或者更新一条 RVOLRecord。
//
// 如果同一个 OpenTime 已经存在：
//
//	更新旧记录。
//
// 如果不存在：
//
//	添加新记录。
//
// 最终只保存最近 30 根。
func (h *RVOLHistory) AddOrUpdate(record RVOLRecord) {
	// 先查找是否存在相同 K 线。
	for i := range h.Records {
		if h.Records[i].OpenTime == record.OpenTime {
			h.Records[i] = record
			return
		}
	}

	// 不存在，直接追加。
	h.Records = append(
		h.Records,
		record,
	)

	// 按时间从旧到新排序。
	sort.Slice(
		h.Records,
		func(i, j int) bool {
			return h.Records[i].OpenTime <
				h.Records[j].OpenTime
		},
	)

	// 最多保存 30 根。
	if len(h.Records) > 30 {
		h.Records = h.Records[len(h.Records)-30:]
	}
}

// Latest 返回最新的一条 RVOLRecord。
func (h *RVOLHistory) Latest() *RVOLRecord {
	if len(h.Records) == 0 {
		return nil
	}

	return &h.Records[len(h.Records)-1]
}

// Previous 返回倒数第二条 RVOLRecord。
func (h *RVOLHistory) Previous() *RVOLRecord {
	if len(h.Records) < 2 {
		return nil
	}

	return &h.Records[len(h.Records)-2]
}

// AverageVolume 计算最近 count 根 K 线的平均成交量。
func (h *RVOLHistory) AverageVolume(
	count int,
) (float64, bool) {

	if count <= 0 {
		return 0, false
	}

	// 历史数据不够。
	if len(h.Records) < count {
		return 0, false
	}

	start := len(h.Records) - count

	var sum float64

	for i := start; i < len(h.Records); i++ {
		sum += h.Records[i].Volume
	}

	average := sum / float64(count)

	return average, true
}
