package imageupdate

import "strings"

// ContainerImage preserves the user-facing image reference while exposing its
// repository, registry, tag, and digest components to update resolution.
type ContainerImage struct {
	ImageName   string
	RegistryURL string
	ImageTag    *ImageTag

	repository string
}

// ImageTag is the mutable portion of a container image reference.
type ImageTag struct {
	TagName   string
	TagDigest string
}

// NewContainerImage parses an image reference without expanding familiar short
// names such as nginx into their fully qualified Docker Hub form.
func NewContainerImage(identifier string) *ContainerImage {
	repositoryAndTag, digest, _ := strings.Cut(strings.TrimSpace(identifier), "@")
	repository := repositoryAndTag
	tagName := ""
	if colon := strings.LastIndex(repositoryAndTag, ":"); colon > strings.LastIndex(repositoryAndTag, "/") {
		repository = repositoryAndTag[:colon]
		tagName = repositoryAndTag[colon+1:]
	}

	registryURL := ""
	imageName := repository
	if slash := strings.Index(repository, "/"); slash >= 0 {
		first := repository[:slash]
		if strings.ContainsAny(first, ".:") || first == "localhost" {
			registryURL = first
			imageName = repository[slash+1:]
		}
	}

	return &ContainerImage{
		ImageName:   imageName,
		RegistryURL: registryURL,
		ImageTag:    &ImageTag{TagName: tagName, TagDigest: digest},
		repository:  repository,
	}
}

// GetFullNameWithoutTag returns the repository exactly as the user specified it.
func (i *ContainerImage) GetFullNameWithoutTag() string {
	if i == nil {
		return ""
	}
	return i.repository
}

// WithTag returns a copy of the image with a replacement tag and digest.
func (i *ContainerImage) WithTag(tag ImageTag) *ContainerImage {
	clone := *i
	clone.ImageTag = &tag
	return &clone
}

// GetFullNameWithTag renders the image reference without normalizing its registry.
func (i *ContainerImage) GetFullNameWithTag() string {
	if i == nil {
		return ""
	}
	result := i.repository
	if i.ImageTag != nil && i.ImageTag.TagName != "" {
		result += ":" + i.ImageTag.TagName
	}
	if i.ImageTag != nil && i.ImageTag.TagDigest != "" {
		result += "@" + i.ImageTag.TagDigest
	}
	return result
}
