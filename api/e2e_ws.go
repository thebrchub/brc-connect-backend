//go:build ignore
// +build ignore

// Quick e2e WebSocket test — run with: go run e2e_ws.go
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/websocket"
	chatpb "github.com/shivanand-burli/go-starter-kit/chat/chatpb"
	"google.golang.org/protobuf/proto"
)

const base = "http://localhost:8080"

func login(email, password string) (string, string) {
	body := fmt.Sprintf(`{"email":"%s","password":"%s"}`, email, password)
	resp, err := http.Post(base+"/auth/login", "application/json", bytes.NewBufferString(body))
	if err != nil {
		log.Fatalf("login failed: %v", err)
	}
	defer resp.Body.Close()
	var result struct {
		AccessToken string `json:"access_token"`
		User        struct {
			ID   string `json:"id"`
			Role string `json:"role"`
		} `json:"user"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	fmt.Printf("  LOGIN %s -> id=%s role=%s\n", email, result.User.ID, result.User.Role)
	return result.AccessToken, result.User.ID
}

func connectWS(token string) *websocket.Conn {
	u := fmt.Sprintf("ws://localhost:8080/ws?token=%s", token)
	conn, resp, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		if resp != nil {
			body := make([]byte, 1024)
			n, _ := resp.Body.Read(body)
			log.Fatalf("ws dial failed: %v status=%d body=%s", err, resp.StatusCode, string(body[:n]))
		}
		log.Fatalf("ws dial failed: %v (no response)", err)
	}
	return conn
}

// wsReader runs a background goroutine that reads from the websocket and
// sends decoded envelopes to the returned channel. This avoids poisoning
// gorilla's internal readErr via SetReadDeadline timeouts.
func wsReader(conn *websocket.Conn) <-chan *chatpb.Envelope {
	ch := make(chan *chatpb.Envelope, 64)
	go func() {
		defer close(ch)
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var env chatpb.Envelope
			if err := proto.Unmarshal(data, &env); err != nil {
				log.Printf("  proto unmarshal failed: %v", err)
				continue
			}
			ch <- &env
		}
	}()
	return ch
}

// waitMsg waits for a message on the channel with a timeout.
func waitMsg(ch <-chan *chatpb.Envelope, timeout time.Duration) *chatpb.Envelope {
	select {
	case env, ok := <-ch:
		if !ok {
			return nil
		}
		return env
	case <-time.After(timeout):
		return nil
	}
}

// drainMsgs reads all available messages from the channel within a short window.
func drainMsgs(ch <-chan *chatpb.Envelope, window time.Duration) int {
	count := 0
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return count
			}
			count++
		case <-time.After(window):
			return count
		}
	}
}

func sendMsg(conn *websocket.Conn, roomID, content string) {
	chatMsg := &chatpb.ChatMessage{Text: content}
	innerData, err := proto.Marshal(chatMsg)
	if err != nil {
		log.Fatalf("proto marshal chatmsg failed: %v", err)
	}
	env := &chatpb.Envelope{
		Type:    "chat_message",
		RoomId:  roomID,
		Payload: innerData,
	}
	data, err := proto.Marshal(env)
	if err != nil {
		log.Fatalf("proto marshal failed: %v", err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		log.Fatalf("ws write failed: %v", err)
	}
}

func sendPing(conn *websocket.Conn) {
	env := &chatpb.Envelope{Type: "ping"}
	data, _ := proto.Marshal(env)
	conn.WriteMessage(websocket.BinaryMessage, data)
}

func decodePayloadText(env *chatpb.Envelope) string {
	if len(env.Payload) == 0 {
		return ""
	}
	var chatMsg chatpb.ChatMessage
	if err := proto.Unmarshal(env.Payload, &chatMsg); err != nil {
		return fmt.Sprintf("(raw:%x)", env.Payload)
	}
	return chatMsg.Text
}

func main() {
	fmt.Println("=== E2E WEBSOCKET TEST ===")
	failed := false

	// Login users
	fmt.Println("\n--- Login ---")
	emp1Token, emp1ID := login("chatalice@brc.com", "alice12345")
	emp2Token, emp2ID := login("chatbob@brc.com", "bob1234567")
	_ = emp1ID

	// Create DM room
	fmt.Println("\n--- Create DM Room ---")
	dmBody := fmt.Sprintf(`{"user_id":"%s"}`, emp2ID)
	dmReq, _ := http.NewRequest("POST", base+"/chat/rooms/dm", bytes.NewBufferString(dmBody))
	dmReq.Header.Set("Authorization", "Bearer "+emp1Token)
	dmReq.Header.Set("Content-Type", "application/json")
	dmResp, err := http.DefaultClient.Do(dmReq)
	if err != nil {
		log.Fatal(err)
	}
	defer dmResp.Body.Close()
	var dmRoom struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	json.NewDecoder(dmResp.Body).Decode(&dmRoom)
	dmRoomID := dmRoom.ID
	if dmRoomID == "" {
		fmt.Println("  FAIL: could not create/get DM room")
		os.Exit(1)
	}
	fmt.Printf("  DM Room: %s type=%s\n", dmRoomID, dmRoom.Type)

	// Connect both users via WS with background readers
	fmt.Println("\n--- Connect WebSockets ---")
	ws1 := connectWS(emp1Token)
	defer ws1.Close()
	ch1 := wsReader(ws1)
	fmt.Println("  emp1 connected")

	ws2 := connectWS(emp2Token)
	defer ws2.Close()
	ch2 := wsReader(ws2)
	fmt.Println("  emp2 connected")

	// Ping/pong test
	fmt.Println("\n--- Ping/Pong Test ---")
	sendPing(ws1)
	pong := waitMsg(ch1, 5*time.Second)
	if pong != nil && pong.Type == "pong" {
		fmt.Println("  PASS: pong received")
	} else {
		fmt.Println("  FAIL: no pong")
		failed = true
	}

	// Wait for room joins to complete
	time.Sleep(1 * time.Second)

	// Drain any initial messages (presence, etc.)
	d1 := drainMsgs(ch1, 500*time.Millisecond)
	d2 := drainMsgs(ch2, 500*time.Millisecond)
	fmt.Printf("  drained emp1=%d emp2=%d\n", d1, d2)

	// emp1 sends a message to the DM room
	fmt.Println("\n--- Send Message: emp1 -> DM ---")
	sendMsg(ws1, dmRoomID, "Hello from Alice!")
	fmt.Println("  sent: Hello from Alice!")

	// emp2 should receive it
	fmt.Println("\n--- Receive on emp2 ---")
	received := waitMsg(ch2, 5*time.Second)
	if received != nil && received.Type == "chat_message" {
		fmt.Printf("  PASS: received type=%s room=%s text=%s\n", received.Type, received.RoomId, decodePayloadText(received))
	} else if received != nil {
		fmt.Printf("  WARN: unexpected type=%s\n", received.Type)
	} else {
		fmt.Println("  FAIL: no message received on emp2")
		failed = true
	}

	// emp1 should also get an echo (delivered by pub/sub fan-out)
	echo := waitMsg(ch1, 5*time.Second)
	if echo != nil && echo.Type == "chat_message" {
		fmt.Printf("  PASS: emp1 echo type=%s text=%s\n", echo.Type, decodePayloadText(echo))
	} else {
		fmt.Println("  WARN: no echo on emp1 (may be OK depending on config)")
	}

	// emp2 sends a reply
	fmt.Println("\n--- Send Reply: emp2 -> DM ---")
	sendMsg(ws2, dmRoomID, "Hey Alice, Bob here!")
	fmt.Println("  sent: Hey Alice, Bob here!")

	received2 := waitMsg(ch1, 5*time.Second)
	if received2 != nil && received2.Type == "chat_message" {
		fmt.Printf("  PASS: received type=%s text=%s\n", received2.Type, decodePayloadText(received2))
	} else {
		fmt.Println("  FAIL: no reply received on emp1")
		failed = true
	}

	// emp2 echo
	echo2 := waitMsg(ch2, 5*time.Second)
	if echo2 != nil && echo2.Type == "chat_message" {
		fmt.Printf("  PASS: emp2 echo type=%s text=%s\n", echo2.Type, decodePayloadText(echo2))
	} else {
		fmt.Println("  WARN: no echo on emp2")
	}

	// Second ping/pong (verify connection still alive)
	fmt.Println("\n--- Ping/Pong (post-message) ---")
	sendPing(ws1)
	pong2 := waitMsg(ch1, 3*time.Second)
	if pong2 != nil && pong2.Type == "pong" {
		fmt.Println("  PASS: pong received")
	} else {
		fmt.Println("  FAIL: no pong after messages")
		failed = true
	}

	// Check message history after flush
	fmt.Println("\n--- Wait for flush + Check History ---")
	time.Sleep(2 * time.Second)
	req2, _ := http.NewRequest("GET", base+"/chat/rooms/"+dmRoomID+"/messages?limit=10", nil)
	req2.Header.Set("Authorization", "Bearer "+emp1Token)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		log.Fatal(err)
	}
	defer resp2.Body.Close()
	var histResult struct {
		Messages []struct {
			Content  string `json:"content"`
			SenderID string `json:"sender_id"`
		} `json:"messages"`
	}
	json.NewDecoder(resp2.Body).Decode(&histResult)
	fmt.Printf("  Messages in DB: %d\n", len(histResult.Messages))
	for i, m := range histResult.Messages {
		if i >= 4 {
			fmt.Printf("    ... (%d more)\n", len(histResult.Messages)-4)
			break
		}
		fmt.Printf("    [%d] sender=%s content=%s\n", i, m.SenderID[:8]+"...", m.Content)
	}

	if failed {
		fmt.Println("\n=== SOME TESTS FAILED ===")
		os.Exit(1)
	}
	fmt.Println("\n=== ALL E2E TESTS PASSED ===")
}
