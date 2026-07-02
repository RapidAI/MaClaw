package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const aliyunDypnsEndpoint = "https://dypnsapi.aliyuncs.com/"

type aliyunDypnsClient struct {
	AccessKeyID     string
	AccessKeySecret string
	HTTPClient      *http.Client
}

type aliyunDypnsResponse struct {
	Code      string          `json:"Code"`
	Message   string          `json:"Message"`
	Success   bool            `json:"Success"`
	Model     json.RawMessage `json:"Model,omitempty"`
	RequestID string          `json:"RequestId,omitempty"`
}

type aliyunSMSCheckModel struct {
	VerifyResult string `json:"VerifyResult"`
}

func (c aliyunDypnsClient) SendVerifyCode(ctx context.Context, req aliyunSMSVerifyCodeSendRequest) error {
	params := map[string]string{
		"CountryCode":   "86",
		"PhoneNumber":   req.PhoneNumber,
		"SignName":      req.SignName,
		"TemplateCode":  req.TemplateCode,
		"TemplateParam": req.TemplateParam,
	}
	if req.CodeLength > 0 {
		params["CodeLength"] = fmt.Sprintf("%d", req.CodeLength)
	}
	resp, err := c.call(ctx, registrationSMSSendVerifyCodeActionName, params)
	if err != nil {
		return err
	}
	if resp.Code != "OK" || !resp.Success {
		return fmt.Errorf("aliyun send sms verify code failed: %s %s", resp.Code, resp.Message)
	}
	return nil
}

func (c aliyunDypnsClient) CheckVerifyCode(ctx context.Context, req aliyunSMSVerifyCodeCheckRequest) (bool, error) {
	resp, err := c.call(ctx, registrationSMSCheckVerifyCodeActionName, map[string]string{
		"CountryCode": "86",
		"PhoneNumber": req.PhoneNumber,
		"VerifyCode":  req.VerifyCode,
	})
	if err != nil {
		return false, err
	}
	if resp.Code != "OK" || !resp.Success {
		return false, fmt.Errorf("aliyun check sms verify code failed: %s %s", resp.Code, resp.Message)
	}
	var model aliyunSMSCheckModel
	if len(resp.Model) > 0 {
		if err := json.Unmarshal(resp.Model, &model); err != nil {
			return false, err
		}
	}
	return strings.EqualFold(strings.TrimSpace(model.VerifyResult), "PASS"), nil
}

func (c aliyunDypnsClient) call(ctx context.Context, action string, params map[string]string) (aliyunDypnsResponse, error) {
	if strings.TrimSpace(c.AccessKeyID) == "" || strings.TrimSpace(c.AccessKeySecret) == "" {
		return aliyunDypnsResponse{}, errInvalidRegistrationAuth("Aliyun AccessKey ID and AccessKey Secret are required")
	}
	values := map[string]string{
		"Action":           action,
		"Version":          "2017-05-25",
		"Format":           "JSON",
		"AccessKeyId":      strings.TrimSpace(c.AccessKeyID),
		"SignatureMethod":  "HMAC-SHA1",
		"SignatureVersion": "1.0",
		"SignatureNonce":   uuid.NewString(),
		"Timestamp":        time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	}
	for k, v := range params {
		values[k] = v
	}
	canonical := canonicalAliyunQuery(values)
	mac := hmac.New(sha1.New, []byte(strings.TrimSpace(c.AccessKeySecret)+"&"))
	_, _ = mac.Write([]byte("POST&%2F&" + aliyunPercentEncode(canonical)))
	values["Signature"] = base64.StdEncoding.EncodeToString(mac.Sum(nil))

	form := url.Values{}
	for k, v := range values {
		form.Set(k, v)
	}
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, aliyunDypnsEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return aliyunDypnsResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return aliyunDypnsResponse{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return aliyunDypnsResponse{}, err
	}
	var out aliyunDypnsResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return aliyunDypnsResponse{}, err
	}
	return out, nil
}

func canonicalAliyunQuery(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, aliyunPercentEncode(k)+"="+aliyunPercentEncode(values[k]))
	}
	return strings.Join(parts, "&")
}

func aliyunPercentEncode(value string) string {
	encoded := url.QueryEscape(value)
	encoded = strings.ReplaceAll(encoded, "+", "%20")
	encoded = strings.ReplaceAll(encoded, "*", "%2A")
	encoded = strings.ReplaceAll(encoded, "%7E", "~")
	return encoded
}
