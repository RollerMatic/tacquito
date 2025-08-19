/*
 Copyright (c) Facebook, Inc. and its affiliates.

 This source code is licensed under the MIT license found in the
 LICENSE file in the root directory of this source tree.
*/

// Package stringy behaves in a similar way to the tacplus cisco/shurbbery implementation
// it's just string matching + regex and hope
package stringy

import (
	"context"

	tq "github.com/facebookincubator/tacquito"
	"github.com/facebookincubator/tacquito/cmds/server/config"
)

// loggerProvider provides the logging implementation
type loggerProvider interface {
	Infof(ctx context.Context, format string, args ...interface{})
	Errorf(ctx context.Context, format string, args ...interface{})
	Debugf(ctx context.Context, format string, args ...interface{})
}

// Option is a functional option for the authorizer
type Option func(*Authorizer)

// EnableCmdV2 is a functional option to enable the new CommandBasedAuthorizerV2 instance
func EnableCmdV2(enableV2 bool) Option {
	return func(a *Authorizer) {
		a.enableCmdV2 = enableV2
	}
}

// New stringy Authorizer
func New(l loggerProvider, opts ...Option) *Authorizer {
	a := &Authorizer{loggerProvider: l}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Authorizer is for authorization of commands and such
type Authorizer struct {
	loggerProvider
	user config.User

	// enableCmdV2 is a flag to enable the new CommandBasedAuthorizerV2 instance
	enableCmdV2 bool
}

// New creates a new stringy authorizer which implements tq.Handler
func (a Authorizer) New(user config.User) (tq.Handler, error) {
	// ReduceAll appends all group level services and commands to the user level
	// user level overrides for services and commands are processed first, then the groups.
	a.ReduceAll(&user)
	return &Authorizer{
		loggerProvider: a.loggerProvider,
		user:           user,
		enableCmdV2:    a.enableCmdV2,
	}, nil
}

// ReduceAll will collapse all services and commands down to the user level
func (a Authorizer) ReduceAll(u *config.User) {
	for _, g := range u.Groups {
		u.Services = append(u.Services, g.Services...)
		u.Commands = append(u.Commands, g.Commands...)
	}
}

// Handle handles all authenticate message types, scoped to the uid
func (a Authorizer) Handle(response tq.Response, request tq.Request) {
	var body tq.AuthorRequest
	if err := tq.Unmarshal(request.Body, &body); err != nil {
		stringyHandleUnexpectedPacket.Inc()
		stringyHandleAuthorizeError.Inc()
		response.Reply(
			tq.NewAuthorReply(
				tq.SetAuthorReplyStatus(tq.AuthorStatusError),
				tq.SetAuthorReplyServerMsg("unable to decode AuthorRequest packet"),
			),
		)
		return
	}

	if a.enableCmdV2 {
		if authorizer := NewCommandBasedAuthorizerV2(request.Context, a.loggerProvider, body, a.user); authorizer != nil {
			a.Debugf(request.Context, "detected user [%v] using command based authorization", a.user.Name)
			authorizer.Handle(response, request)
			return
		}
	} else {
		if authorizer := NewCommandBasedAuthorizer(request.Context, a.loggerProvider, body, a.user); authorizer != nil {
			a.Debugf(request.Context, "detected user [%v] using command based authorization", a.user.Name)
			authorizer.Handle(response, request)
			return
		}
	}

	if authorizer := NewSessionBasedAuthorizer(request.Context, a.loggerProvider, body, a.user); authorizer != nil {
		a.Debugf(request.Context, "detected user [%v] using session based authorization", a.user.Name)
		authorizer.Handle(response, request)
		return
	}

	a.Debugf(request.Context, "failed to authorize the user: [%v]", a.user.Name)
	stringyHandleAuthorizeFail.Inc()
	response.Reply(
		tq.NewAuthorReply(
			tq.SetAuthorReplyStatus(tq.AuthorStatusFail),
			tq.SetAuthorReplyServerMsg("not authorized"),
		),
	)
}
