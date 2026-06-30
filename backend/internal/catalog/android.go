// Package catalog defines the curated set of Android versions that OpenCuttles
// can deploy. Each version maps to a Cuttlefish build that "cvd fetch" knows how
// to download by branch and build target.
package catalog

import "github.com/opencuttles/opencuttles/backend/internal/domain"

// androidVersions is the ordered catalog surfaced in the deploy dropdown. The
// first entry is treated as the default when a request omits a version.
// Build target names differ by branch family: aosp-main builds
// "aosp_cf_x86_64_phone", while the release and GSI branches build
// "aosp_cf_x86_64_only_phone". These combinations are fetchable anonymously via
// "cvd fetch" (no Google credentials required).
var androidVersions = []domain.AndroidVersion{
	{
		ID:          "latest-release",
		Label:       "Android (latest stable release)",
		Branch:      "aosp-android-latest-release",
		BuildTarget: "aosp_cf_x86_64_only_phone-userdebug",
		Description: "Latest stable AOSP release build. Recommended default.",
	},
	{
		ID:          "aosp-main",
		Label:       "Android (latest, aosp-main trunk)",
		Branch:      "aosp-main",
		BuildTarget: "aosp_cf_x86_64_phone-userdebug",
		Description: "Bleeding-edge AOSP trunk; the newest build may occasionally lack artifacts.",
	},
	{
		ID:          "android15",
		Label:       "Android 15 (GSI)",
		Branch:      "aosp-android15-gsi",
		BuildTarget: "aosp_cf_x86_64_only_phone-userdebug",
		Description: "Android 15 generic system image.",
	},
	{
		ID:          "android14",
		Label:       "Android 14 (GSI)",
		Branch:      "aosp-android14-gsi",
		BuildTarget: "aosp_cf_x86_64_only_phone-userdebug",
		Description: "Android 14 generic system image.",
	},
	{
		ID:          "android13",
		Label:       "Android 13 (GSI)",
		Branch:      "aosp-android13-gsi",
		BuildTarget: "aosp_cf_x86_64_only_phone-userdebug",
		Description: "Android 13 generic system image.",
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
