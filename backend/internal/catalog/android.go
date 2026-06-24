// Package catalog defines the curated set of Android versions that OpenCuttles
// can deploy. Each version maps to a Cuttlefish build that "cvd fetch" knows how
// to download by branch and build target.
package catalog

import "github.com/opencuttles/opencuttles/backend/internal/domain"

// androidVersions is the ordered catalog surfaced in the deploy dropdown. The
// first entry is treated as the default when a request omits a version.
var androidVersions = []domain.AndroidVersion{
	{
		ID:          "aosp-main",
		Label:       "Android (aosp-main, latest)",
		Branch:      "aosp-main",
		BuildTarget: "aosp_cf_x86_64_phone-trunk_staging-userdebug",
		Description: "Latest AOSP trunk build.",
	},
	{
		ID:          "android15",
		Label:       "Android 15 (GSI)",
		Branch:      "aosp-android15-gsi",
		BuildTarget: "aosp_cf_x86_64_phone-userdebug",
		Description: "Android 15 generic system image.",
	},
	{
		ID:          "android14",
		Label:       "Android 14 (GSI)",
		Branch:      "aosp-android14-gsi",
		BuildTarget: "aosp_cf_x86_64_phone-userdebug",
		Description: "Android 14 generic system image.",
	},
	{
		ID:          "android13",
		Label:       "Android 13 (GSI)",
		Branch:      "aosp-android13-gsi",
		BuildTarget: "aosp_cf_x86_64_phone-userdebug",
		Description: "Android 13 generic system image.",
	},
	{
		ID:          "android12",
		Label:       "Android 12 (GSI)",
		Branch:      "aosp-android12-gsi",
		BuildTarget: "aosp_cf_x86_64_phone-userdebug",
		Description: "Android 12 generic system image.",
	},
}

// AndroidVersions returns a copy of the curated version catalog.
func AndroidVersions() []domain.AndroidVersion {
	out := make([]domain.AndroidVersion, len(androidVersions))
	copy(out, androidVersions)
	return out
}

// Default returns the catalog entry used when no version is specified.
func Default() domain.AndroidVersion {
	return androidVersions[0]
}

// Lookup finds a version by its catalog id.
func Lookup(id string) (domain.AndroidVersion, bool) {
	for _, version := range androidVersions {
		if version.ID == id {
			return version, true
		}
	}
	return domain.AndroidVersion{}, false
}

// DefaultBuild renders the "branch/build_target" string consumed by
// "cvd fetch --default_build".
func DefaultBuild(version domain.AndroidVersion) string {
	return version.Branch + "/" + version.BuildTarget
}
