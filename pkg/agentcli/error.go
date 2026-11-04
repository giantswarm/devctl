package agentcli

// RemoteError is a failed service request, distinct from invalid command
// arguments. It preserves the underlying error so wait engines can still
// recognize exhausted retries and continue polling until their deadline.
type RemoteError struct {
	Err error
}

func (e *RemoteError) Error() string { return e.Err.Error() }

func (e *RemoteError) Unwrap() error { return e.Err }

// Cause preserves microerror's error-kind matching through this wrapper.
func (e *RemoteError) Cause() error { return e.Err }
