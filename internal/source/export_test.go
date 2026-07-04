package source

// SetMonobankBaseURL overrides the Monobank API base URL for tests.
var SetMonobankBaseURL = func(u string) { monobankBaseURL = u }
