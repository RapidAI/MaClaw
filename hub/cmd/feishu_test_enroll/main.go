// Command feishu_test_enroll is a manual integration test for the Feishu
// Contact v3 API. It verifies tenant token acquisition, department lookup,
// user existence check, and user creation.
//
// Usage:
//
//	go run ./hub/cmd/feishu_test_enroll \
//	  -app-id=cli_xxx -app-secret=xxx \
//	  -email=user@example.com -mobile=+8613800138000 \
//	  -name=TestUser -dept-id=0
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

var (
	flagAppID     = flag.String("app-id", "", "Feishu app ID (required)")
	flagAppSecret = flag.String("app-secret", "", "Feishu app secret (required)")
	flagEmail     = flag.String("email", "", "User email to enroll (required)")
	flagMobile    = flag.String("mobile", "", "User mobile with country code, e.g. +8613800138000")
	flagName      = flag.String("name", "", "Display name (defaults to email local part)")
	flagDeptID    = flag.String("dept-id", "0", "Target department ID")
	flagAPIBase   = flag.String("api-base", "https://open.feishu.cn", "Feishu API base URL")
)

var client = &http.Client{Timeout: 15 * time.Second}

func main() {
	flag.Parse()

	if *flagAppID == "" || *flagAppSecret == "" || *flagEmail == "" {
		fmt.Fprintln(os.Stderr, "Usage: feishu_test_enroll -app-id=... -app-secret=... -email=...")
		flag.PrintDefaults()
		os.Exit(1)
	}

	apiBase := *flagAPIBase

	// Step 1: Get tenant_access_token
	fmt.Println("=== Step 1: 获取 tenant_access_token ===")
	token, err := getTenantToken(apiBase, *flagAppID, *flagAppSecret)
	if err != nil {
		fmt.Fprintf(os.Stderr, "获取 token 失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("token: %s...%s\n\n", token[:8], token[len(token)-4:])

	// Step 2: Check if user already exists
	fmt.Println("=== Step 2: 检查用户是否已存在 ===")
	openID, err := lookupByEmail(apiBase, token, *flagEmail)
	if err != nil {
		fmt.Printf(" 邮箱查找失败: %v\n", err)
	}
	if openID != "" {
		fmt.Printf("用户已存在, open_id=%s\n", openID)
		fmt.Println("无需创建，测试完成。")
		return
	}
	fmt.Println("用户不存在，继续创建...")

	// Step 3: Create user
	name := *flagName
	if name == "" {
		for i, c := range *flagEmail {
			if c == '@' {
				name = (*flagEmail)[:i]
				break
			}
		}
		if name == "" {
			name = *flagEmail
		}
	}

	fmt.Println("=== Step 3: 创建用户 ===")
	newOpenID, err := createUser(apiBase, token, *flagEmail, *flagMobile, name, *flagDeptID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "创建用户失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("用户创建成功! open_id=%s\n", newOpenID)
}

func getTenantToken(apiBase, appID, appSecret string) (string, error) {
	body, _ := json.Marshal(map[string]string{
		"app_id":     appID,
		"app_secret": appSecret,
	})
	resp, err := client.Post(apiBase+"/open-apis/auth/v3/tenant_access_token/internal", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.Code != 0 {
		return "", fmt.Errorf("code=%d msg=%s", result.Code, result.Msg)
	}
	return result.TenantAccessToken, nil
}

func lookupByEmail(apiBase, token, email string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"emails": []string{email},
	})
	req, _ := http.NewRequest("POST", apiBase+"/open-apis/contact/v3/users/batch_get_id?user_id_type=open_id", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	fmt.Printf("邮箱查找响应: %s\n\n", string(raw))

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			UserList []struct {
				UserID string `json:"user_id"`
			} `json:"user_list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", err
	}
	if result.Code != 0 {
		return "", fmt.Errorf("code=%d msg=%s", result.Code, result.Msg)
	}
	if len(result.Data.UserList) > 0 && result.Data.UserList[0].UserID != "" {
		return result.Data.UserList[0].UserID, nil
	}
	return "", nil
}

func createUser(apiBase, token, email, mobile, name, deptID string) (string, error) {
	payload := map[string]any{
		"name":           name,
		"email":          email,
		"department_ids": []string{deptID},
		"employee_type":  1,
	}
	if mobile != "" {
		payload["mobile"] = mobile
	}
	body, _ := json.Marshal(payload)
	fmt.Printf("创建用户请求: %s\n", string(body))

	req, _ := http.NewRequest("POST", apiBase+"/open-apis/contact/v3/users?user_id_type=open_id&department_id_type=department_id", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	fmt.Printf("创建用户响应: %s\n\n", string(raw))

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			User struct {
				OpenID string `json:"open_id"`
			} `json:"user"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", err
	}
	if result.Code != 0 {
		return "", fmt.Errorf("code=%d msg=%s", result.Code, result.Msg)
	}
	return result.Data.User.OpenID, nil
}
