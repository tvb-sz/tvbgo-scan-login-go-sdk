package tvbgoScanLoginGoSdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ErrResponseNotOK 當請求響應碼非200時返回的錯誤
//   - 調用方只關注響應碼爲200的場景時，直接判斷err是否爲nil即可
//     result, err := client.JSON(xx,xx,xx)
//     if err != nil {
//     return
//     }
//     // your code // http響應碼爲200時的邏輯
//     ------------------------------------------------------------------------
//   - 調用方若需處理非200時返回值，如下處理：
//     if err != nil && errors.Is(err, guzzle.ErrResponseNotOK) {
//     // http響應碼非200，此時result也是有值的
//     }
var ErrResponseNotOK = errors.New("failed response status code is not equal 200")

// defaultUserAgent 默認UA頭，調用方法時可覆蓋
var defaultUserAgent = "guzzle/go"

// Result 響應封裝
type Result struct {
	StatusCode    int         // 響應碼
	ContentLength int64       // 響應長度
	Header        http.Header // 響應頭
	Body          []byte      // 讀取出來的響應body體字節內容
}

// httpClient http客戶端相關方法封裝
type httpClient struct {
	client *http.Client
}

// newClient 創建一個http客戶端實例對象
//   - client *http.Client 可以自定義http請求的相關參數例如請求超時控制，使用默認則傳 nil
func newClient(client *http.Client) *httpClient {
	if client == nil {
		client = http.DefaultClient
	}

	return &httpClient{
		client: client,
	}
}

// NewRequest 新建http請求，鏈式初始化請求，需鏈式 Do 方法才實際執行<比較底層的方法>
//   - method 請求方法：GET、POST等，使用 http.MethodGet http.MethodPost 等常量
//   - url    請求完整URL
//   - body   請求body體 io.Reader 類型
func (c *httpClient) NewRequest(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	// set default user-agent | key is case-insensitive
	// <header頭的名稱是不區分大小寫的>
	req.Header.Set("User-Agent", defaultUserAgent)

	// 設置請求context
	req = req.WithContext(ctx)

	return req, nil
}

// Do 處理請求：用於鏈式調用
func (c *httpClient) Do(req *http.Request) (result Result, err error) {
	res, err := c.client.Do(req)
	if err != nil {
		return result, err
	}

	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(res.Body)

	row, err := io.ReadAll(res.Body)
	if err != nil {
		return result, err
	}

	// 非200時返回錯誤同時結果集仍然返回內容，以方便調用方需要處理狀態碼非200的場景
	if res.StatusCode != http.StatusOK {
		err = ErrResponseNotOK
	}

	// set result
	result.Body = row
	result.StatusCode = res.StatusCode
	result.Header = res.Header
	result.ContentLength = res.ContentLength

	return result, err
}

// Request 執行請求：實際執行請求<比較底層的方法>
//   - method 請求方法：GET、POST等，使用 http.MethodGet http.MethodPost 等常量
//   - url    請求完整URL
//   - body   請求body體 io.Reader 類型
//   - head   請求header部分
func (c *httpClient) Request(ctx context.Context, method, url string, body io.Reader, head map[string]string) (Result, error) {
	req, err := c.NewRequest(ctx, method, url, body)
	if err != nil {
		return Result{}, err
	}
	for key, val := range head {
		req.Header.Add(key, val)
	}
	return c.Do(req)
}

// Get 執行 get 請求
//   - url    請求完整URL
//   - query  GET請求URl中的Query鍵值對，支持類型：map[string]string、map[string][]string<等價於 url.Values>
//   - head   請求header部分鍵值對
//   - 注意 url 與 query是完全分開傳參，沒有查詢參數query給 nil 即可
func (c *httpClient) Get(ctx context.Context, url string, query any, head map[string]string) (Result, error) {
	if query != nil {
		url += "?" + BuildQuery(query)
	}
	req, err := c.NewRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Result{}, err
	}
	for key, val := range head {
		req.Header.Add(key, val)
	}
	return c.Do(req)
}

// Delete 執行 delete 請求
//   - url    請求完整URL
//   - query  GET請求URl中的Query鍵值對，支持類型：map[string]string、map[string][]string<等價於 url.Values>
//   - head   請求header部分鍵值對
//   - 注意 url 與 query是完全分開傳參，沒有查詢參數query給 nil 即可
func (c *httpClient) Delete(ctx context.Context, url string, param any, head map[string]string) (Result, error) {
	if param != nil {
		url += "?" + BuildQuery(param)
	}
	req, err := c.NewRequest(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return Result{}, err
	}

	for key, val := range head {
		req.Header.Add(key, val)
	}

	return c.Do(req)
}

// JSON 執行 post/put/patch/delete 請求，采用 json 格式<比較底層的方法>
//   - method 請求方法：GET、POST等，使用 http.MethodGet http.MethodPost 等常量
//   - url    請求完整URL
//   - body   請求body體 io.Reader 類型
//   - head   請求header部分鍵值對
func (c *httpClient) JSON(ctx context.Context, method, url string, body io.Reader, head map[string]string) (Result, error) {
	req, err := c.NewRequest(ctx, method, url, body)
	if err != nil {
		return Result{}, err
	}

	for key, val := range head {
		req.Header.Add(key, val)
	}
	req.Header.Set("Content-Type", "application/json")

	return c.Do(req)
}

// Form 執行 post 請求，采用 form 表單格式<比較底層的方法>
//   - method 請求方法：GET、POST等，使用 http.MethodGet http.MethodPost 等常量
//   - url    請求完整URL
//   - body   請求body體 io.Reader 類型
//   - head   請求header部分鍵值對
func (c *httpClient) Form(ctx context.Context, method, url string, body io.Reader, head map[string]string) (Result, error) {
	req, err := c.NewRequest(ctx, method, url, body)
	if err != nil {
		return Result{}, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for key, val := range head {
		req.Header.Add(key, val)
	}

	return c.Do(req)
}

// PostJSON 執行 post 請求，采用 json 格式
//   - url    請求完整URL，自主處理好query
//   - body   請求body體，支持：字符串、字節數組、結構體等，最終會轉換爲 io.Reader 類型
//   - head   請求header部分鍵值對，無傳nil
func (c *httpClient) PostJSON(ctx context.Context, url string, body any, head map[string]string) (Result, error) {
	return c.JSON(ctx, http.MethodPost, url, toJsonReader(body), head)
}

// PutJSON 執行 put 請求，采用 json 格式
//   - url    請求完整URL，自主處理好query
//   - body   請求body體，支持：字符串、字節數組、結構體等，最終會轉換爲 io.Reader 類型
//   - head   請求header部分鍵值對，無傳nil
func (c *httpClient) PutJSON(ctx context.Context, url string, body any, head map[string]string) (Result, error) {
	return c.JSON(ctx, http.MethodPut, url, toJsonReader(body), head)
}

// PatchJSON 執行 patch 請求，采用 json 格式
//   - url    請求完整URL，自主處理好query
//   - body   請求body體，支持：字符串、字節數組、結構體等，最終會轉換爲 io.Reader 類型
//   - head   請求header部分鍵值對，無傳nil
func (c *httpClient) PatchJSON(ctx context.Context, url string, body any, head map[string]string) (Result, error) {
	return c.JSON(ctx, http.MethodPatch, url, toJsonReader(body), head)
}

// DeleteJSON 執行 delete 請求，采用 json 格式
//   - url    請求完整URL，自主處理好query
//   - body   請求body體，支持：字符串、字節數組、結構體等，最終會轉換爲 io.Reader 類型
//   - head   請求header部分鍵值對，無傳nil
func (c *httpClient) DeleteJSON(ctx context.Context, url string, body any, head map[string]string) (Result, error) {
	return c.JSON(ctx, http.MethodDelete, url, toJsonReader(body), head)
}

// PostForm 執行 post 請求，采用 form 格式
//   - url    請求完整URL，自主處理好query
//   - body   請求body體，支持：字符串、字節數組、結構體等，最終會轉換爲 io.Reader 類型
//   - head   請求header部分鍵值對，無傳nil
func (c *httpClient) PostForm(ctx context.Context, url string, body any, head map[string]string) (Result, error) {
	return c.Form(ctx, http.MethodPost, url, toFormReader(body), head)
}

// PutForm 執行 put 請求，采用 form 格式
//   - url    請求完整URL，自主處理好query
//   - body   請求body體，支持：字符串、字節數組、結構體等，最終會轉換爲 io.Reader 類型
//   - head   請求header部分鍵值對，無傳nil
func (c *httpClient) PutForm(ctx context.Context, url string, body any, head map[string]string) (Result, error) {
	return c.Form(ctx, http.MethodPut, url, toFormReader(body), head)
}

// PatchForm 執行 patch 請求，采用 form 格式
//   - url    請求完整URL，自主處理好query
//   - body   請求body體，支持：字符串、字節數組、結構體等，最終會轉換爲 io.Reader 類型
//   - head   請求header部分鍵值對，無傳nil
func (c *httpClient) PatchForm(ctx context.Context, url string, body any, head map[string]string) (Result, error) {
	return c.Form(ctx, http.MethodPatch, url, toFormReader(body), head)
}

// DeleteForm 執行 delete 請求，采用 form 格式
//   - url    請求完整URL，自主處理好query
//   - body   請求body體，支持：字符串、字節數組、結構體等，最終會轉換爲 io.Reader 類型
//   - head   請求header部分鍵值對，無傳nil
func (c *httpClient) DeleteForm(ctx context.Context, url string, body any, head map[string]string) (Result, error) {
	return c.Form(ctx, http.MethodDelete, url, toFormReader(body), head)
}

// toJsonReader 處理參數爲JSON類型
func toJsonReader(param any) io.Reader {
	switch pv := param.(type) {
	case nil:
		return nil
	case io.Reader:
		return pv
	case string:
		return strings.NewReader(pv)
	case []byte:
		return bytes.NewReader(pv)
	default:
		b, _ := json.Marshal(param)
		return bytes.NewReader(b)
	}
}

// toFormReader 處理參數爲Form表單類型
//   - 支持的參數類型如下：
//   - nil
//   - io.Reader
//   - string
//   - []byte
//   - map[string]string
//   - map[string][]string <==> url.Values
func toFormReader(param any) io.Reader {
	switch pv := param.(type) {
	case nil:
		return nil
	case io.Reader:
		return pv
	case string:
		return strings.NewReader(pv)
	case []byte:
		return bytes.NewReader(pv)
	case map[string]string, map[string][]string, url.Values:
		return strings.NewReader(BuildQuery(pv))
	default:
		return http.NoBody
	}
}

// BuildQuery 處理請求參數爲URL裏的Query鍵值對
//   - 支持的能構建的參數類型如下：
//   - map[string]string
//   - map[string][]string <==> url.Values
//   - 除了上述不支持的類型，其他類型將會忽略返回空字符串
func BuildQuery(param any) string {
	switch pv := param.(type) {
	case map[string]string:
		values := make(url.Values)
		for k, v := range pv {
			values.Add(k, v)
		}
		return values.Encode()
	case map[string][]string:
		values := url.Values(pv)
		return values.Encode()
	case url.Values:
		return pv.Encode()
	default:
		return ""
	}
}
