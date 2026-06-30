// Package catalog defines the curated set of Android versions that OpenCuttles
// can deploy. Each version maps to a Cuttlefish build that "cvd fetch" knows how
// to download by branch and build target.
package catalog

import "github.com/opencuttles/opencuttles/backend/internal/domain"

// androidVersions is the ordered catalog surfaced in the deploy dropdown. The
// first entry is treated as the default when a request omits a version.
//
// Every entry MUST correspond to a real branch on the Android CI
// (ci.android.com) that publishes the "aosp_cf_x86_64_only_phone-userdebug"
// Cuttlefish device target and is fetchable anonymously via "cvd fetch" (no
// Google credentials). Two important constraints learned the hard way:
//
//   - Per-version GSI branches were only cut through Android 14
//     (aosp-android14-gsi). There is no aosp-android15-gsi / aosp-android16-gsi
//     branch; Android 15 and newer are served by aosp-android-latest-release.
//     Listing a nonexistent branch makes the build API return HTTP 400
//     ("Unable to create build ... typo in the branch or target name?").
//   - The older non-"only" target name (aosp_cf_x86_64_phone) is no longer
//     produced for these branches, so only the "_only_phone" target is used.
//
// Branch existence can be checked with:
//
//	curl -so /dev/null -w '%{http_code}' \
//	  https://ci.android.com/builds/branches/<branch>/grid?legacy=1
//
// (200 = exists, 404 = does not).
var androidVersions = []domain.AndroidVersion{
	{
		ID:          "latest-release",
		Label:       "Android (latest stable release)",
		Branch:      "aosp-android-latest-release",
		BuildTarget: "aosp_cf_x86_64_only_phone-userdebug",
		Description: "Latest stable AOSP release (currently Android 15/16). Recommended default.",
	},
	{
		ID:          "aosp-main",
		Label:       "Android (preview, aosp-main trunk)",
		Branch:      "aosp-main",
		BuildTarget: "aosp_cf_x86_64_only_phone-userdebug",
		Description: "Bleeding-edge AOSP trunk; the newest build may occasionally lack artifacts.",
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
	{
		ID:          "android12",
		Label:       "Android 12 (GSI)",
		Branch:      "aosp-android12-gsi",
		BuildTarget: "aosp_cf_x86_64_only_phone-userdebug",
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
