package util_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"math"

	"github.com/nifcloud/nifcloud-additional-storage-csi-driver/pkg/util"
)

var _ = Describe("util", func() {
	Describe("roundUpSize", func() {
		DescribeTable(
			"valid",
			func(volumeSizeBytes int64, allocationUnitBytes int64, expected int64) {
				actual, err := util.RoundUpSize(volumeSizeBytes, allocationUnitBytes)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(actual).Should(Equal(expected))
			},
			Entry("round up 1000 / 100", int64(1000), int64(100), int64(10)),
			Entry("round up 1001 / 100", int64(1001), int64(100), int64(11)),
			Entry("round up 1100 / 100", int64(1100), int64(100), int64(11)),
			Entry("round up 1101 / 100", int64(1101), int64(100), int64(12)),
			Entry("round up 0 / 100", int64(0), int64(100), int64(0)),
			Entry("round up (MaxInt64 - 1) / 2", int64(math.MaxInt64-1), int64(2), int64((math.MaxInt64-1)/2)),
		)

		DescribeTable(
			"invalid",
			func(volumeSizeBytes int64, allocationUnitBytes int64) {
				_, err := util.RoundUpSize(volumeSizeBytes, allocationUnitBytes)
				Expect(err).Should(HaveOccurred())
			},
			Entry("round up 1000 / 0", int64(1000), int64(0)),
			Entry("round up -1000 / 100", int64(-1000), int64(100)),
			Entry("round up 1000 / -100", int64(1000), int64(-100)),
			Entry("round up MaxInt64 / 2 occurs oeverflow", int64(math.MaxInt64), int64(2)),
		)
	})
})
