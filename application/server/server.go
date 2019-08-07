// Sshwifty - A Web SSH client
//
// Copyright (C) 2019 Rui NI <nirui@gmx.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, either version 3 of the
// License, or (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package server

import (
	"context"
	"crypto/tls"
	"errors"
	goLog "log"
	"net"
	"net/http"
	"strconv"
	"sync"

	"github.com/niruix/sshwifty/application/configuration"
	"github.com/niruix/sshwifty/application/log"
)

type dumpWrite struct{}

func (d dumpWrite) Write(b []byte) (int, error) {
	return len(b), nil
}

// Errors
var (
	ErrInvalidIPAddress = errors.New(
		"Invalid IP address")
)

// HandlerBuilder builds a HTTP handler
type HandlerBuilder func(
	commonCfg configuration.Common,
	cfg configuration.Server,
	logger log.Logger) http.Handler

// CloseCallback will be called when the server has closed
type CloseCallback func(error)

// Server represents a server
type Server struct {
	logger       log.Logger
	cfg          configuration.Common
	shutdownWait *sync.WaitGroup
}

// Serving represents a server that is serving for requests
type Serving struct {
	server       http.Server
	shutdownWait *sync.WaitGroup
}

// New creates a new Server builder
func New(logger log.Logger) Server {
	return Server{
		logger:       logger,
		shutdownWait: &sync.WaitGroup{},
	}
}

// Serve starts serving
func (s Server) Serve(
	commonCfg configuration.Common,
	serverCfg configuration.Server,
	closeCallback CloseCallback,
	handlerBuilder HandlerBuilder,
) *Serving {
	ccCfg := commonCfg.WithDefault()
	ssCfg := serverCfg.WithDefault()

	l := s.logger.Context(
		"Server (%s:%d)", ssCfg.ListenInterface, ssCfg.ListenPort)

	ss := &Serving{
		server: http.Server{
			Handler:           handlerBuilder(ccCfg, ssCfg, l),
			ReadTimeout:       ssCfg.ReadTimeout,
			ReadHeaderTimeout: ssCfg.InitialTimeout,
			WriteTimeout:      ssCfg.WriteTimeout,
			IdleTimeout:       ssCfg.ReadTimeout,
			MaxHeaderBytes:    4096,
			ErrorLog:          goLog.New(dumpWrite{}, "", 0),
		},
		shutdownWait: s.shutdownWait,
	}

	s.shutdownWait.Add(1)

	go ss.run(l, ssCfg, closeCallback)

	return ss
}

// Wait waits until all server is closed
func (s Server) Wait() {
	s.shutdownWait.Wait()
}

func (s *Serving) buildListener(
	ip string, port uint16) (*net.TCPListener, error) {
	ipAddr := net.ParseIP(ip)

	if ipAddr == nil {
		return nil, ErrInvalidIPAddress
	}

	ipPort := net.JoinHostPort(
		ipAddr.String(), strconv.FormatInt(int64(port), 10))

	addr, addrErr := net.ResolveTCPAddr("tcp", ipPort)

	if addrErr != nil {
		return nil, addrErr
	}

	return net.ListenTCP("tcp", addr)
}

// run starts the server
func (s *Serving) run(
	logger log.Logger,
	cfg configuration.Server,
	closeCallback CloseCallback,
) error {
	var err error
	var ls *net.TCPListener

	defer func() {
		if err == nil || err == http.ErrServerClosed {
			logger.Info("Closed")
		} else {
			logger.Warning("Failed to serve due to error: %s", err)
		}

		s.shutdownWait.Done()

		closeCallback(err)
	}()

	ls, err = s.buildListener(cfg.ListenInterface, cfg.ListenPort)

	if err != nil {
		return err
	}

	defer ls.Close()

	logger.Info("Serving")

	if !cfg.IsTLS() {
		err = s.server.Serve(ls)

		if err == nil {
			return nil
		}

		return err
	}

	if s.server.TLSConfig != nil {
		s.server.TLSConfig.MinVersion = tls.VersionTLS12
	}

	err = s.server.ServeTLS(
		ls, cfg.TLSCertificateFile, cfg.TLSCertificateKeyFile)

	if err == nil {
		return nil
	}

	return err
}

// Close close the server
func (s *Serving) Close() error {
	return s.server.Shutdown(context.TODO())
}
