package pipes

import (
	"net/http"
	"sync"

	"github.com/concourse/atc"
	"github.com/concourse/atc/db"
	"github.com/pivotal-golang/lager"
	"github.com/tedsuo/rata"
)

type Server struct {
	logger lager.Logger

	url         string
	externalURL string

	pipes  map[string]pipe
	pipesL *sync.RWMutex

	db PipeDB
}

//go:generate counterfeiter . PipeDB

type PipeDB interface {
	CreatePipe(pipeGUID string, url string) error
	GetPipe(pipeGUID string) (db.Pipe, error)
}

func NewServer(logger lager.Logger, url string, externalURL string, db PipeDB) *Server {
	return &Server{
		logger: logger,

		url:         url,
		externalURL: externalURL,

		pipes:  make(map[string]pipe),
		pipesL: new(sync.RWMutex),
		db:     db,
	}
}

func (s *Server) forwardRequest(w http.ResponseWriter, r *http.Request, host string, route string, pipeID string) (*http.Response, error) {
	generator := rata.NewRequestGenerator(host, atc.Routes)

	req, err := generator.CreateRequest(
		route,
		rata.Params{"pipe_id": pipeID},
		r.Body,
	)

	if err != nil {
		return nil, err
	}

	req.Header = r.Header

	client := &http.Client{
		Transport: &http.Transport{},
	}

	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	return response, nil
}
