package reservation

import "github.com/giantswarm/microerror"

var invalidConfigError = &microerror.Error{
	Kind: "invalidConfigError",
}

// IsInvalidConfig asserts invalidConfigError.
func IsInvalidConfig(err error) bool {
	return microerror.Cause(err) == invalidConfigError
}

// clusterNotFoundError indicates that the GitOps repo holds no such management
// cluster.
var clusterNotFoundError = &microerror.Error{
	Kind: "clusterNotFoundError",
}

// IsClusterNotFound asserts clusterNotFoundError.
func IsClusterNotFound(err error) bool {
	return microerror.Cause(err) == clusterNotFoundError
}

// clusterNotEnabledError indicates that the management cluster carries no
// reservations ConfigMap, which is the per-cluster opt-in.
var clusterNotEnabledError = &microerror.Error{
	Kind: "clusterNotEnabledError",
}

// IsClusterNotEnabled asserts clusterNotEnabledError.
func IsClusterNotEnabled(err error) bool {
	return microerror.Cause(err) == clusterNotEnabledError
}

// appNotFoundError indicates that the rendered collections of the cluster hold
// no source object for the app.
var appNotFoundError = &microerror.Error{
	Kind: "appNotFoundError",
}

// IsAppNotFound asserts appNotFoundError.
func IsAppNotFound(err error) bool {
	return microerror.Cause(err) == appNotFoundError
}

// renderError indicates that the cluster's collections could not be rendered.
var renderError = &microerror.Error{
	Kind: "renderError",
}

// IsRender asserts renderError.
func IsRender(err error) bool {
	return microerror.Cause(err) == renderError
}

// renderAssertionError indicates that the rendered collections do not carry the
// reservation. A reservation that does not survive the render is a silent no-op
// on the cluster, so it must never reach a commit.
var renderAssertionError = &microerror.Error{
	Kind: "renderAssertionError",
}

// IsRenderAssertion asserts renderAssertionError.
func IsRenderAssertion(err error) bool {
	return microerror.Cause(err) == renderAssertionError
}

// alreadyReservedError indicates that the app already holds a reservation on the
// cluster. One reservation per (app, cluster) is the lock.
var alreadyReservedError = &microerror.Error{
	Kind: "alreadyReservedError",
}

// IsAlreadyReserved asserts alreadyReservedError.
func IsAlreadyReserved(err error) bool {
	return microerror.Cause(err) == alreadyReservedError
}

// appNotSupportedError indicates that the app was located but is shaped in a way
// this version cannot move: an extras app, or a release carrying its chart
// inline.
var appNotSupportedError = &microerror.Error{
	Kind: "appNotSupportedError",
}

// IsAppNotSupported asserts appNotSupportedError.
func IsAppNotSupported(err error) bool {
	return microerror.Cause(err) == appNotSupportedError
}

// invalidDurationError indicates that the requested duration is not a duration
// this version accepts, or is longer than the cluster allows. The message, not
// the kind, carries which of the two it is: a caller reports both the same way.
var invalidDurationError = &microerror.Error{
	Kind: "invalidDurationError",
}

// IsInvalidDuration asserts invalidDurationError.
func IsInvalidDuration(err error) bool {
	return microerror.Cause(err) == invalidDurationError
}

// appAmbiguousError indicates that the app repository holds several charts and
// the caller named none of them.
var appAmbiguousError = &microerror.Error{
	Kind: "appAmbiguousError",
}

// IsAppAmbiguous asserts appAmbiguousError.
func IsAppAmbiguous(err error) bool {
	return microerror.Cause(err) == appAmbiguousError
}

// notReservedError indicates that the app holds no reservation on the cluster,
// so there is nothing for Release to undo.
var notReservedError = &microerror.Error{
	Kind: "notReservedError",
}

// IsNotReserved asserts notReservedError.
func IsNotReserved(err error) bool {
	return microerror.Cause(err) == notReservedError
}
