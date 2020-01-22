/*
Copyright 2017 Crunchy Data Solutions, Inc.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package connect

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"

	"github.com/crunchydata/crunchy-proxy/config"
	"github.com/crunchydata/crunchy-proxy/protocol"
	"github.com/crunchydata/crunchy-proxy/util/log"
)

func Send(connection net.Conn, message []byte) (int, error) {
	return connection.Write(message)
}

// ReceiveStartupMessage reads a startup message from a connection. Startup
// messages are a special case in that they do not have a 'Message Type' byte.
// Therefore, they must be treated differently than all other messages.
func ReceiveStartupMessage(connection net.Conn) ([]byte, int, error) {
	buffer := bytes.Buffer{}

	header := make([]byte, 4)
	if _, err := io.ReadFull(connection, header); err != nil {
		return nil, 0, err
	}

	buffer.Write(header)

	// Determine the number of bytes to read. The value of 'length' is
	// inclusive of the bytes representing the length, so we will subtract
	// those bytes from the value.
	n := int(binary.BigEndian.Uint32(header[:])) - 4

	payload := make([]byte, n)
	if _, err := io.ReadFull(connection, payload); err != nil {
		return nil, 0, err
	}

	buffer.Write(payload)

	return buffer.Bytes(), buffer.Len(), nil
}

// Receive reads a normal FE/BE message.
func Receive(connection net.Conn) ([]byte, int, error) {
	buffer := bytes.Buffer{}

	header := make([]byte, 5)
	if _, err := io.ReadFull(connection, header); err != nil {
		return nil, 0, err
	}

	buffer.Write(header)

	// Determine the number of bytes to read. The value of 'length' is
	// inclusive of the bytes representing the length, so we will subtract
	// those bytes from the value.
	n := int(binary.BigEndian.Uint32(header[1:5])) - 4

	payload := make([]byte, n)
	if n, err := io.ReadFull(connection, payload); err != nil {
		return nil, n, err
	}

	buffer.Write(payload)

	return buffer.Bytes(), buffer.Len(), nil
}

func Connect(host string) (net.Conn, error) {
	connection, err := net.Dial("tcp", host)

	if err != nil {
		return nil, err
	}

	if config.GetBool("credentials.ssl.enable") {
		log.Info("SSL connections are enabled.")

		/*
		 * First determine if SSL is allowed by the backend. To do this, send an
		 * SSL request. The response from the backend will be a single byte
		 * message. If the value is 'S', then SSL connections are allowed and an
		 * upgrade to the connection should be attempted. If the value is 'N',
		 * then the backend does not support SSL connections.
		 */

		/* Create the SSL request message. */
		message := protocol.NewMessageBuffer([]byte{})
		message.WriteInt32(8)
		message.WriteInt32(protocol.SSLRequestCode)

		/* Send the SSL request message. */
		_, err := connection.Write(message.Bytes())

		if err != nil {
			log.Error("Error sending SSL request to backend.")
			log.Errorf("Error: %s", err.Error())
			return nil, err
		}

		/* Receive SSL response message. */
		response := make([]byte, 4096)
		_, err = connection.Read(response)

		if err != nil {
			log.Error("Error receiving SSL response from backend.")
			log.Errorf("Error: %s", err.Error())
			return nil, err
		}

		/*
		 * If SSL is not allowed by the backend then close the connection and
		 * throw an error.
		 */
		if len(response) > 0 && response[0] != 'S' {
			log.Error("The backend does not allow SSL connections.")
			connection.Close()
		} else {
			log.Debug("SSL connections are allowed by PostgreSQL.")
			log.Debug("Attempting to upgrade connection.")
			connection = UpgradeClientConnection(host, connection)
			log.Debug("Connection successfully upgraded.")
		}
	}

	return connection, nil
}
