package disk

import "math"

// SmartCounters are the SMART values that mean the same for all kinds of disks (ATA, NVMe, SCSI), so disks can be
// compared and sorted by them. Each is -1 if the disk does not report it.
type SmartCounters struct {
	// BadSectors are the sectors the disk could not use any more or not read: ATA reallocated, pending and offline
	// uncorrectable sectors (attributes 5, 197, 198), the media errors of NVMe drives, the grown defects of SCSI disks
	BadSectors int64
	// Uncorrectable are the errors the disk could not correct: ATA reported uncorrectable errors (187), the total
	// uncorrected errors of SCSI disks. NVMe drives count them as media errors (BadSectors).
	Uncorrectable int64
	// CrcErrors are the transfer errors between the disk and the controller (ATA 199), e.g. of a bad cable
	CrcErrors int64
	// WearPercent is how much of the rated endurance of an SSD is used: "percentage used" of NVMe drives, the
	// normalized wear attributes of ATA SSDs (see wearAttributes). May exceed 100.
	WearPercent int64
	// BytesWritten is how much was written to the disk: the data units of NVMe drives, ATA attribute 241 in the unit
	// its name tells (see bytesWritten)
	BytesWritten int64
	// PowerCycles is how often the disk was switched on (ATA 12, NVMe)
	PowerCycles int64
	// UnsafeShutdowns is how often the disk lost power without being told before (NVMe, ATA 174 of SSDs, 192 of hard
	// disks: "power-off retract", the heads parked by the power loss)
	UnsafeShutdowns int64
	// LoadCycles is how often a hard disk parked its heads (ATA 193): some park them after seconds of idling and wear
	// out by that (rated for 300 000 to 600 000)
	LoadCycles int64
}

// unknownCounters are counters of a disk that reports none.
var unknownCounters = SmartCounters{BadSectors: -1, Uncorrectable: -1, CrcErrors: -1, WearPercent: -1, BytesWritten: -1,
	PowerCycles: -1, UnsafeShutdowns: -1, LoadCycles: -1}

// wearAttributes are the ATA attributes of SSDs whose normalized value is the endurance left in percent (starting at
// 100), by their name: the id alone is not enough, vendors use the same ids for other values.
var wearAttributes = map[string]bool{
	"Wear_Leveling_Count":     true, // 177, e.g. Samsung
	"Percent_Lifetime_Remain": true, // 202, e.g. Crucial, Micron
	"SSD_Life_Left":           true, // 231, e.g. Kingston, SandForce
	"Media_Wearout_Indicator": true, // 233, e.g. Intel
}

// bytesWritten returns the bytes written to an ATA disk from attribute 241, by the unit its name tells, false if the
// unit is not known.
func bytesWritten(name string, raw int64, logicalBlockSize int64) (int64, bool) {
	unit := int64(0)
	switch name {
	case "Total_LBAs_Written":
		unit = logicalBlockSize
		if unit <= 0 {
			unit = 512
		}
	case "Host_Writes_32MiB":
		unit = 32 << 20
	case "Host_Writes_GiB", "Lifetime_Writes_GiB", "Total_Writes_GiB":
		unit = 1 << 30
	default:
		return 0, false
	}
	return multiply(raw, unit), true
}

// multiply returns a * b, the largest int64 if larger.
func multiply(a int64, b int64) int64 {
	if a > 0 && b > math.MaxInt64/a {
		return math.MaxInt64
	}
	return a * b
}

// add adds the value to the counter, which may be unknown (-1).
func add(counter *int64, value int64) {
	*counter = max(*counter, 0) + value
}

// countersOf returns the counters of the output of smartctl.
func countersOf(parsed *smartctlOutput) SmartCounters {
	counters := unknownCounters
	if parsed.AtaSmartAttributes != nil {
		for _, attribute := range parsed.AtaSmartAttributes.Table {
			raw := jsonCount(attribute.Raw.Value)
			switch {
			case attribute.Id == 5 || attribute.Id == 197 || attribute.Id == 198:
				add(&counters.BadSectors, raw)
			case attribute.Id == 187:
				counters.Uncorrectable = raw
			case attribute.Id == 199:
				counters.CrcErrors = raw
			case attribute.Id == 12:
				counters.PowerCycles = raw
			case attribute.Id == 174 || attribute.Id == 192:
				add(&counters.UnsafeShutdowns, raw)
			case attribute.Id == 193:
				counters.LoadCycles = raw
			case attribute.Id == 241:
				if written, ok := bytesWritten(attribute.Name, raw, parsed.LogicalBlockSize); ok {
					counters.BytesWritten = written
				}
			case wearAttributes[attribute.Name] && attribute.Value >= 0 && attribute.Value <= 100:
				counters.WearPercent = int64(100 - attribute.Value)
			}
		}
	}
	if log := parsed.NvmeLog; log != nil {
		counters.BadSectors = jsonCount(log.MediaErrors)
		counters.WearPercent = int64(log.PercentageUsed)
		// units of 1000 blocks of 512 bytes
		counters.BytesWritten = multiply(jsonCount(log.DataUnitsWritten), 512_000)
		counters.PowerCycles = jsonCount(log.PowerCycles)
		counters.UnsafeShutdowns = jsonCount(log.UnsafeShutdowns)
	}
	if parsed.ScsiGrownDefectList != nil {
		counters.BadSectors = jsonCount(*parsed.ScsiGrownDefectList)
	}
	for _, counter := range parsed.ScsiErrorCounterLog {
		add(&counters.Uncorrectable, jsonCount(counter.TotalUncorrectedErrors))
	}
	return counters
}
