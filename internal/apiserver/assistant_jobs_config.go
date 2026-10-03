package apiserver

func assistantJobsEnabled(configured *bool, legacyEnv string) bool {
	if configured != nil {
		return *configured
	}
	return legacyEnv == "true"
}
