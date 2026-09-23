package domain

import "errors"

var ErrPluginAdminGatewayUnavailable = errors.New("Gateway Plugin Admin API is unavailable")
var ErrPluginAdminInvalidRequest = errors.New("Plugin Admin request is invalid")
