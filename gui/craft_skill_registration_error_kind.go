package main

import "strings"

type craftSkillRegistrationErrorKind int

const (
	craftSkillRegistrationErrorUnknown craftSkillRegistrationErrorKind = iota
	craftSkillRegistrationErrorAlreadyExists
)

func classifyCraftSkillRegistrationError(err error) craftSkillRegistrationErrorKind {
	if err == nil {
		return craftSkillRegistrationErrorUnknown
	}
	if strings.Contains(err.Error(), "already exists") {
		return craftSkillRegistrationErrorAlreadyExists
	}
	return craftSkillRegistrationErrorUnknown
}

func (k craftSkillRegistrationErrorKind) NeedsUniqueNameRetry() bool {
	return k == craftSkillRegistrationErrorAlreadyExists
}
