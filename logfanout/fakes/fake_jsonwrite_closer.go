// This file was generated by counterfeiter
package fakes

import (
	"sync"

	"github.com/concourse/atc/logfanout"
)

type FakeJSONWriteCloser struct {
	WriteJSONStub        func(interface{}) error
	writeJSONMutex       sync.RWMutex
	writeJSONArgsForCall []struct {
		arg1 interface{}
	}
	writeJSONReturns struct {
		result1 error
	}
	CloseStub        func() error
	closeMutex       sync.RWMutex
	closeArgsForCall []struct{}
	closeReturns struct {
		result1 error
	}
}

func (fake *FakeJSONWriteCloser) WriteJSON(arg1 interface{}) error {
	fake.writeJSONMutex.Lock()
	fake.writeJSONArgsForCall = append(fake.writeJSONArgsForCall, struct {
		arg1 interface{}
	}{arg1})
	fake.writeJSONMutex.Unlock()
	if fake.WriteJSONStub != nil {
		return fake.WriteJSONStub(arg1)
	} else {
		return fake.writeJSONReturns.result1
	}
}

func (fake *FakeJSONWriteCloser) WriteJSONCallCount() int {
	fake.writeJSONMutex.RLock()
	defer fake.writeJSONMutex.RUnlock()
	return len(fake.writeJSONArgsForCall)
}

func (fake *FakeJSONWriteCloser) WriteJSONArgsForCall(i int) interface{} {
	fake.writeJSONMutex.RLock()
	defer fake.writeJSONMutex.RUnlock()
	return fake.writeJSONArgsForCall[i].arg1
}

func (fake *FakeJSONWriteCloser) WriteJSONReturns(result1 error) {
	fake.WriteJSONStub = nil
	fake.writeJSONReturns = struct {
		result1 error
	}{result1}
}

func (fake *FakeJSONWriteCloser) Close() error {
	fake.closeMutex.Lock()
	fake.closeArgsForCall = append(fake.closeArgsForCall, struct{}{})
	fake.closeMutex.Unlock()
	if fake.CloseStub != nil {
		return fake.CloseStub()
	} else {
		return fake.closeReturns.result1
	}
}

func (fake *FakeJSONWriteCloser) CloseCallCount() int {
	fake.closeMutex.RLock()
	defer fake.closeMutex.RUnlock()
	return len(fake.closeArgsForCall)
}

func (fake *FakeJSONWriteCloser) CloseReturns(result1 error) {
	fake.CloseStub = nil
	fake.closeReturns = struct {
		result1 error
	}{result1}
}

var _ logfanout.JSONWriteCloser = new(FakeJSONWriteCloser)