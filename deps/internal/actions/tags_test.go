package actions

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Git tag references", func() {
	const first = "1111111111111111111111111111111111111111"
	const second = "2222222222222222222222222222222222222222"
	const third = "3333333333333333333333333333333333333333"

	It("peels annotated tags and sorts exact releases", func() {
		set, err := ParseTags(first + "\trefs/tags/v4\n" + first + "\trefs/tags/v4.2.2\n" + second + "\trefs/tags/v5.0.0-rc.1\n" + third + "\trefs/tags/v5.0.0\n" + first + "\trefs/tags/v6.0.0\n" + third + "\trefs/tags/v6.0.0^{}\n")
		Expect(err).NotTo(HaveOccurred())
		Expect(set.Versions()).To(Equal([]string{"v6.0.0", "v5.0.0", "v5.0.0-rc.1", "v4.2.2"}))
		resolved, err := set.Resolve("v6.0.0")
		Expect(err).NotTo(HaveOccurred())
		Expect(resolved).To(Equal(third))
	})

	It("maps a SHA pin to its most specific matching version and validates its comment", func() {
		set, err := ParseTags(first + "\trefs/tags/v4\n" + first + "\trefs/tags/v4.2.2\n" + second + "\trefs/tags/v5.0.0\n")
		Expect(err).NotTo(HaveOccurred())
		version, err := set.CurrentVersion(first, "v4.2.2")
		Expect(err).NotTo(HaveOccurred())
		Expect(version).To(Equal("v4.2.2"))
		_, err = set.CurrentVersion(first, "v5.0.0")
		Expect(err).To(MatchError(ContainSubstring("does not resolve")))
		_, err = set.CurrentVersion(third, "")
		Expect(err).To(MatchError(ContainSubstring("does not match a version tag")))
	})
})
