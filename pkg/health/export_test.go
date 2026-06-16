package health

// ProbeURLForTest exposes the unexported probeURL helper to the external test
// package so the probe logic can be exercised against an httptest server.
var ProbeURLForTest = probeURL
