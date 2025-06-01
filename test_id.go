package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/fredcamaral/gomcp-sdk/server"
	"github.com/fredcamaral/gomcp-sdk/transport"
)

func main() {
	srv := server.NewServer("test-server", "1.0.0")
	httpTransport := transport.NewHTTPTransport(&transport.HTTPConfig{Address: ":0"})
	srv.SetTransport(httpTransport)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Start(ctx); err \!= nil {
		log.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)
	addr := httpTransport.Address()
	
	reqBody := `{"id":"test-789","not_a_method":"value"}`
	resp, _ := http.Post(fmt.Sprintf("http://%s", addr), "application/json", strings.NewReader(reqBody))
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	
	if result["id"] == "test-789" {
		fmt.Println("✅ ID extraction SUCCESS\!")
	} else {
		fmt.Printf("❌ ID extraction FAILED\! Got: %v
", result["id"])
	}
}
