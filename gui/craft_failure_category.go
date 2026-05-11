package main

type craftFailureCategory string

const (
	craftFailureCategoryEnvironment craftFailureCategory = "environment"
	craftFailureCategoryPermission  craftFailureCategory = "permission"
	craftFailureCategoryCapability  craftFailureCategory = "capability"
	craftFailureCategoryScript      craftFailureCategory = "script"
	craftFailureCategoryArtifact    craftFailureCategory = "artifact"
	craftFailureCategoryUnknown     craftFailureCategory = "unknown"
)

func (category craftFailureCategory) String() string {
	return string(category)
}
