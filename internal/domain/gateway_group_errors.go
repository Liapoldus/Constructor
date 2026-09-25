package domain

import "errors"

var ErrGatewayGroupsUnavailable = errors.New("Gateway groups API is unavailable")
var ErrGatewayGroupQueryInvalid = errors.New("Gateway group query is invalid")
