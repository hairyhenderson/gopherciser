package scenario

import (
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/pkg/errors"
	"github.com/qlik-oss/gopherciser/action"
	"github.com/qlik-oss/gopherciser/connection"
	"github.com/qlik-oss/gopherciser/session"
)

var (
	connTestFuncs []func(*connection.ConnectionSettings, *session.State, *action.State) error
	cfLock        sync.Mutex
)

func init() {
	ResetDefaultConnFuncs()
}

func ResetDefaultConnFuncs() {
	connTestFuncs = make([]func(*connection.ConnectionSettings, *session.State, *action.State) error, 0, 2)
	_ = RegisterConnFuncs([]func(*connection.ConnectionSettings, *session.State, *action.State) error{defaultGuidWsConnectTest, restGetConnectTest})
}

// RegisterConnFuncs registers custom connection functions.
// This should be done as early as possible and must be done before unmarshaling actions
func RegisterConnFuncs(connFuncs []func(*connection.ConnectionSettings, *session.State, *action.State) error) error {
	return errors.WithStack(registerConnFuncs(connFuncs))
}

// RegisterConnFunc registers a custom connection function
// This should be done as early as possible and must be done before unmarshaling actions
func RegisterConnFunc(connFunc func(*connection.ConnectionSettings, *session.State, *action.State) error) error {
	return errors.WithStack(registerConnFunc(connFunc))
}

func GetConnTestFuncs() []func(*connection.ConnectionSettings, *session.State, *action.State) error {
	return connTestFuncs
}

func defaultGuidWsConnectTest(connectionSettings *connection.ConnectionSettings, sessionState *session.State, actionState *action.State) error {
	connectFunc, err := connectionSettings.GetConnectFunc(sessionState, "00000000-0000-0000-0000-000000000000")
	if err != nil {
		return errors.Wrapf(err, "failed to get connect function")
	}

	connectWs := OpenAppSettings{}.GetConnectWsAction("", connectFunc)
	if err := connectWs.Execute(sessionState, connectionSettings); err != nil {
		return errors.Wrap(err, "failed to connect to engine over web socket")
	}
	if sessionState.Wait(actionState) {
		return errors.Wrap(actionState.Errors(), "failed to connect to engine over web socket")
	}

	// Verify connection
	if sessionState.Connection == nil {
		return errors.Errorf("failed to get connection to engine")
	}
	sense := sessionState.Connection.Sense()
	if sense == nil || sense.Global == nil {
		return errors.Errorf("failed to get sense uplink")
	}

	return nil
}

func restGetConnectTest(connectionSettings *connection.ConnectionSettings, sessionState *session.State, actionState *action.State) error {
	host, err := connectionSettings.GetRestUrl()
	if err != nil {
		return errors.Wrap(err, "failed to get REST URL")
	}
	pilotRequest := session.RestRequest{
		Method:      session.GET,
		Destination: fmt.Sprintf("%s/api/v1/locale", host),
	}
	sessionState.Rest.QueueRequest(actionState, true, &pilotRequest, sessionState.LogEntry)
	if sessionState.Wait(actionState) {
		actionError := actionState.Errors()
		return errors.Wrap(actionError, "failed to execute REST request")
	}
	sessionState.Rest.WaitForPending()

	if pilotRequest.ResponseStatusCode != http.StatusOK {
		if strings.Contains(string(pilotRequest.ResponseBody), "<html>") {
			return errors.Errorf("failed response code: %s",
				pilotRequest.ResponseStatus)
		}
		return errors.Errorf("failed response code: %s (%s)",
			pilotRequest.ResponseStatus, pilotRequest.ResponseBody)
	}

	return nil
}

func registerConnFuncs(connFuncs []func(*connection.ConnectionSettings, *session.State, *action.State) error) error {
	for _, connFunc := range connFuncs {
		if err := registerConnFunc(connFunc); err != nil {
			return errors.WithStack(err)
		}
	}

	return nil
}

func registerConnFunc(connFunc func(*connection.ConnectionSettings, *session.State, *action.State) error) error {
	cfLock.Lock()
	defer cfLock.Unlock()

	if connTestFuncs == nil {
		connTestFuncs = make([]func(*connection.ConnectionSettings, *session.State, *action.State) error, 0)
	}

	connTestFuncs = append(connTestFuncs, connFunc)
	return nil
}
