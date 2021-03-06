// This file was generated by counterfeiter
package fakes

import (
	"sync"

	gconn "github.com/cloudfoundry-incubator/garden/client/connection"
	"github.com/concourse/atc/worker"
)

type FakeGardenConnectionFactory struct {
	BuildConnectionStub        func() gconn.Connection
	buildConnectionMutex       sync.RWMutex
	buildConnectionArgsForCall []struct{}
	buildConnectionReturns     struct {
		result1 gconn.Connection
	}
	BuildConnectionFromDBStub        func() (gconn.Connection, error)
	buildConnectionFromDBMutex       sync.RWMutex
	buildConnectionFromDBArgsForCall []struct{}
	buildConnectionFromDBReturns     struct {
		result1 gconn.Connection
		result2 error
	}
}

func (fake *FakeGardenConnectionFactory) BuildConnection() gconn.Connection {
	fake.buildConnectionMutex.Lock()
	fake.buildConnectionArgsForCall = append(fake.buildConnectionArgsForCall, struct{}{})
	fake.buildConnectionMutex.Unlock()
	if fake.BuildConnectionStub != nil {
		return fake.BuildConnectionStub()
	} else {
		return fake.buildConnectionReturns.result1
	}
}

func (fake *FakeGardenConnectionFactory) BuildConnectionCallCount() int {
	fake.buildConnectionMutex.RLock()
	defer fake.buildConnectionMutex.RUnlock()
	return len(fake.buildConnectionArgsForCall)
}

func (fake *FakeGardenConnectionFactory) BuildConnectionReturns(result1 gconn.Connection) {
	fake.BuildConnectionStub = nil
	fake.buildConnectionReturns = struct {
		result1 gconn.Connection
	}{result1}
}

func (fake *FakeGardenConnectionFactory) BuildConnectionFromDB() (gconn.Connection, error) {
	fake.buildConnectionFromDBMutex.Lock()
	fake.buildConnectionFromDBArgsForCall = append(fake.buildConnectionFromDBArgsForCall, struct{}{})
	fake.buildConnectionFromDBMutex.Unlock()
	if fake.BuildConnectionFromDBStub != nil {
		return fake.BuildConnectionFromDBStub()
	} else {
		return fake.buildConnectionFromDBReturns.result1, fake.buildConnectionFromDBReturns.result2
	}
}

func (fake *FakeGardenConnectionFactory) BuildConnectionFromDBCallCount() int {
	fake.buildConnectionFromDBMutex.RLock()
	defer fake.buildConnectionFromDBMutex.RUnlock()
	return len(fake.buildConnectionFromDBArgsForCall)
}

func (fake *FakeGardenConnectionFactory) BuildConnectionFromDBReturns(result1 gconn.Connection, result2 error) {
	fake.BuildConnectionFromDBStub = nil
	fake.buildConnectionFromDBReturns = struct {
		result1 gconn.Connection
		result2 error
	}{result1, result2}
}

var _ worker.GardenConnectionFactory = new(FakeGardenConnectionFactory)
