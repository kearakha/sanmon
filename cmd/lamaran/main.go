// Command lamaran generate match.md (analisa kecocokan skill) buat satu
// lamaran magang, lewat Sanmon. Konsumen pertama Sanmon & sumber trafik live
// feed (PRD §M2). Batas keras: satu file, satu perintah, tanpa DB, tanpa UI.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) != 2 {
		fatalf("usage: lamaran <path-ke-folder-job>")
	}
	jobDir := os.Args[1]

	jd, err := os.ReadFile(filepath.Join(jobDir, "jd.md"))
	if err != nil {
		fatalf("baca jd.md: %v", err)
	}

	workspaceRoot := filepath.Dir(filepath.Dir(filepath.Clean(jobDir)))
	skills, err := os.ReadFile(filepath.Join(workspaceRoot, "skills.md"))
	if err != nil {
		fatalf("baca skills.md: %v", err)
	}
	master, err := os.ReadFile(filepath.Join(workspaceRoot, "master.md"))
	if err != nil {
		fatalf("baca master.md: %v", err)
	}

	virtualKey := os.Getenv("SANMON_VIRTUAL_KEY")
	if virtualKey == "" {
		fatalf("SANMON_VIRTUAL_KEY wajib diisi")
	}
	gatewayURL := os.Getenv("SANMON_URL")
	if gatewayURL == "" {
		gatewayURL = "http://localhost:8777"
	}
	model := os.Getenv("SANMON_MODEL")
	if model == "" {
		model = "gemini-3.6-flash"
	}

	match, err := callChatCompletion(gatewayURL, virtualKey, model, buildPrompt(string(master), string(skills), string(jd)))
	if err != nil {
		fatalf("panggil gateway: %v", err)
	}

	outPath := filepath.Join(jobDir, "match.md")
	if err := os.WriteFile(outPath, []byte(match), 0o644); err != nil {
		fatalf("tulis match.md: %v", err)
	}

	fmt.Println("match.md ditulis ke", outPath)
}

func buildPrompt(master, skills, jd string) string {
	return fmt.Sprintf(`Kamu bantu Rakha analisa kecocokan skill buat lamaran magang backend developer.

ATURAN KERAS (anti-bohong): cuma boleh klaim skill tier Have & Adjacent dari daftar
skill di bawah. Skill Gap TIDAK BOLEH diklaim dikuasai — kalau JD minta skill Gap,
tandai jujur (skip/belajar/akui pas interview).

=== PROFIL (master.md) ===
%s

=== SKILL DENGAN TIER & EVIDENCE (skills.md) ===
%s

=== JOB DESCRIPTION ===
%s

Tulis output markdown, jadi isi file match.md:
1. Ringkasan kecocokan (Strong/Moderate/Weak fit, 1-2 kalimat kenapa)
2. Have — skill yang match JD, sertakan evidence project
3. Adjacent — skill yang defensible, klaim hati-hati
4. Gap — skill yang diminta JD tapi belum dikuasai, kasih rekomendasi (skip/belajar/akui jujur)
5. Catatan buat tailoring CV/cover letter

PENTING: balas LANGSUNG dengan isi markdown-nya, mulai dari heading "#". Jangan
kasih basa-basi pembuka/penutup, jangan bungkus dalam code fence.
`, master, skills, jd)
}

func callChatCompletion(gatewayURL, virtualKey, model, prompt string) (string, error) {
	body, err := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest(http.MethodPost, gatewayURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+virtualKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gateway balikin status %d: %s", resp.StatusCode, respBody)
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("response nggak ada choices: %s", respBody)
	}
	return parsed.Choices[0].Message.Content, nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
