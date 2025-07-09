package main

import (
    "bytes"
    "encoding/json"
    "fmt"
    "html/template"
    "io"
    "log"
    "net/http"
    "os"
    "time"
)

type ChatRequest struct {
    Model  string `json:"model"`
    Prompt string `json:"prompt"`
    Stream bool   `json:"stream"`
}

type ChatResponse struct {
    Response string `json:"response"`
}

const htmlTemplate = `
<!DOCTYPE html>
<html>
<head>
    <title>DeepSeek Local Interface</title>
    <style>
        body { font-family: Arial, sans-serif; max-width: 800px; margin: 0 auto; padding: 20px; }
        .chat-container { border: 1px solid #ddd; height: 400px; overflow-y: auto; padding: 10px; margin-bottom: 10px; }
        .input-container { display: flex; gap: 10px; }
        input[type="text"] { flex: 1; padding: 10px; }
        button { padding: 10px 20px; }
        .message { margin: 10px 0; padding: 10px; border-radius: 5px; }
        .user { background: #e3f2fd; }
        .assistant { background: #f1f8e9; }
    </style>
</head>
<body>
    <h1>🧠 DeepSeek Local Interface</h1>
    <div id="chat-container" class="chat-container"></div>
    <div class="input-container">
        <input type="text" id="prompt-input" placeholder="Ask DeepSeek something...">
        <button onclick="sendMessage()">Send</button>
    </div>
    
    <script>
        async function sendMessage() {
            const input = document.getElementById('prompt-input');
            const prompt = input.value.trim();
            if (!prompt) return;
            
            appendMessage('user', prompt);
            input.value = '';
            
            try {
                const response = await fetch('/chat', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ prompt: prompt })
                });
                
                const data = await response.json();
                appendMessage('assistant', data.response);
            } catch (error) {
                appendMessage('assistant', 'Error: ' + error.message);
            }
        }
        
        function appendMessage(type, content) {
            const container = document.getElementById('chat-container');
            const div = document.createElement('div');
            div.className = 'message ' + type;
            div.textContent = (type === 'user' ? 'You: ' : 'DeepSeek: ') + content;
            container.appendChild(div);
            container.scrollTop = container.scrollHeight;
        }
        
        document.getElementById('prompt-input').addEventListener('keypress', function(e) {
            if (e.key === 'Enter') sendMessage();
        });
    </script>
</body>
</html>
`

func main() {
    ollamaURL := os.Getenv("OLLAMA_URL")
    if ollamaURL == "" {
        ollamaURL = "http://host.docker.internal:11434"
    }

    http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        tmpl := template.Must(template.New("index").Parse(htmlTemplate))
        tmpl.Execute(w, nil)
    })

    http.HandleFunc("/chat", func(w http.ResponseWriter, r *http.Request) {
        if r.Method != "POST" {
            http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
            return
        }

        var req struct {
            Prompt string `json:"prompt"`
        }
        
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            http.Error(w, err.Error(), http.StatusBadRequest)
            return
        }

        chatReq := ChatRequest{
            Model:  "codellama:7b", // Updated to use available model
            Prompt: req.Prompt,
            Stream: false,
        }

        reqBody, _ := json.Marshal(chatReq)
        
        // Add timeout and better error handling
        client := &http.Client{Timeout: 30 * time.Second}
        resp, err := client.Post(ollamaURL+"/api/generate", "application/json", bytes.NewBuffer(reqBody))
        if err != nil {
            log.Printf("Error connecting to Ollama: %v", err)
            http.Error(w, fmt.Sprintf("Cannot connect to Ollama: %v", err), http.StatusInternalServerError)
            return
        }
        defer resp.Body.Close()

        if resp.StatusCode != http.StatusOK {
            body, _ := io.ReadAll(resp.Body)
            log.Printf("Ollama responded with status %d: %s", resp.StatusCode, string(body))
            http.Error(w, fmt.Sprintf("Ollama error: %s", string(body)), http.StatusInternalServerError)
            return
        }

        body, err := io.ReadAll(resp.Body)
        if err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }

        var chatResp ChatResponse
        if err := json.Unmarshal(body, &chatResp); err != nil {
            log.Printf("Failed to parse Ollama response: %s", string(body))
            http.Error(w, fmt.Sprintf("Invalid response from Ollama: %v", err), http.StatusInternalServerError)
            return
        }

        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]string{"response": chatResp.Response})
    })

    port := os.Getenv("PORT")
    if port == "" {
        port = "8080"
    }

    log.Printf("DeepSeek interface starting on port %s", port)
    log.Fatal(http.ListenAndServe(":"+port, nil))
}
