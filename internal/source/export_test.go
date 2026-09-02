package source

// SetMonobankBaseURL overrides the Monobank API base URL for tests.
var SetMonobankBaseURL = func(u string) { monobankBaseURL = u }

// SetNBUBaseURL overrides the National Bank of Ukraine API base URL for tests.
var SetNBUBaseURL = func(u string) { nbuBaseURL = u }

// SetECBBaseURL overrides the ECB reference-rate API base URL for tests.
var SetECBBaseURL = func(u string) { ecbBaseURL = u }
