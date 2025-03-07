package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"

	"github.com/gin-gonic/gin"
)

// **全局变量**
var (
	cachedWebServices []map[string]string // 用于存储扫描结果
	cacheMutex        sync.Mutex          // 互斥锁，保证数据安全
)

// **获取本机 IP**
func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		log.Println("获取本机 IP 失败:", err)
		return "Unknown"
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			log.Println("本机 IP:", ipnet.IP.String())
			return ipnet.IP.String()
		}
	}
	log.Println("未找到本机 IP")
	return "Unknown"
}

// **扫描所有端口**
func scanPorts(ip string) []int {
	var openPorts []int
	var wg sync.WaitGroup
	portChan := make(chan int, 100) // 限制最大并发 100
	resultChan := make(chan int)

	log.Println("开始扫描端口...")

	// **启动扫描任务**
	for i := 0; i < cap(portChan); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for port := range portChan {
				address := net.JoinHostPort(ip, fmt.Sprintf("%d", port))
				conn, err := net.DialTimeout("tcp", address, 500*time.Millisecond)
				if err == nil {
					conn.Close()
					log.Printf("端口开放: %d\n", port)
					resultChan <- port
				}
			}
		}()
	}

	// **发送端口扫描任务**
	go func() {
		for port := 1; port <= 65535; port++ {
			portChan <- port
		}
		close(portChan)
	}()

	// **收集结果**
	go func() {
		for port := range resultChan {
			openPorts = append(openPorts, port)
		}
	}()

	// 等待所有扫描完成
	wg.Wait()
	close(resultChan)
	log.Println("端口扫描完成")
	return openPorts
}

// **检查端口是否为 Web 服务**
func isWebService(ip string, port int) (string, bool) {
	urls := []string{
		fmt.Sprintf("http://%s:%d", ip, port),
		fmt.Sprintf("https://%s:%d", ip, port),
	}

	client := http.Client{Timeout: 2 * time.Second}

	for _, url := range urls {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			continue
		}

		// **模拟 Chrome User-Agent**
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/114.0.5735.110 Safari/537.36")

		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		// **允许 HTML 响应**
		contentType := resp.Header.Get("Content-Type")
		if strings.Contains(contentType, "text/html") {
			return url, true
		}
	}

	return "", false
}

// **获取网页标题 & Favicon**
func getWebTitleAndIcon(ip string, port int) (string, string, bool) {
	url, isWeb := isWebService(ip, port)

	if ip == getLocalIP() && port == 80 {
		return "", "", false
	}
	if !isWeb {
		return "", "", false
	}

	client := http.Client{Timeout: 2 * time.Second}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", "", false
	}

	// **模拟 Chrome User-Agent**
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/114.0.5735.110 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", false
	}
	defer resp.Body.Close()

	// **解析 HTML**
	doc, err := html.Parse(resp.Body)
	if err != nil {
		return "", "", false
	}

	var title, favicon string

	// **递归查找 `<title>`**
	var findTitle func(*html.Node)
	findTitle = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "title" && n.FirstChild != nil {
			title = strings.TrimSpace(n.FirstChild.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findTitle(c)
		}
	}

	// **递归查找 `<link rel="icon">`**
	var findFavicon func(*html.Node)
	findFavicon = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "link" {
			var rel, href string
			for _, attr := range n.Attr {
				if attr.Key == "rel" {
					rel = attr.Val
				}
				if attr.Key == "href" {
					href = attr.Val
				}
			}
			if strings.Contains(rel, "icon") && href != "" {
				if strings.HasPrefix(href, "http") {
					favicon = href
				} else {
					// **拼接完整 URL**
					favicon = fmt.Sprintf("%s%s", url, href)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findFavicon(c)
		}
	}

	findTitle(doc)
	findFavicon(doc)

	// **如果没有找到 Favicon**
	if favicon == "" {
		favicon = "./static/logo.webp"
	}

	// **如果没有 `<title>`，但有 HTML 结构，使用默认值**
	if title == "" {
		title = fmt.Sprintf("未命名网页 (%d)", port)
	}

	return title, favicon, true
}

// **后台任务，每 20 分钟扫描一次**
func updateCache() {
	for {
		ip := getLocalIP()
		openPorts := scanPorts(ip)

		var newWebServices []map[string]string

		for _, port := range openPorts {
			title, icon, isWeb := getWebTitleAndIcon(ip, port)
			if isWeb {
				newWebServices = append(newWebServices, map[string]string{
					"title": title,
					"icon":  icon,
					"link":  fmt.Sprintf("http://%s:%d", ip, port),
					"port":  fmt.Sprintf("%d", port),
				})
			}
		}

		// **更新缓存**
		cacheMutex.Lock()
		cachedWebServices = newWebServices
		cacheMutex.Unlock()

		log.Println("缓存已更新，等待 20 分钟后再次扫描...")
		time.Sleep(20 * time.Minute)
	}
}

// **处理 HTTP 请求**
func handleRequest(c *gin.Context) {
	cacheMutex.Lock()
	defer cacheMutex.Unlock()
	noServices := len(cachedWebServices) == 0
	c.HTML(http.StatusOK, "index.html", gin.H{
		"ip":          getLocalIP(),
		"webServices": cachedWebServices,
		"noServices":  noServices,
	})
}

func main() {
	r := gin.Default()

	// **设置静态文件目录**
	r.Static("/static", "./static")

	// **加载 HTML 模板**
	r.LoadHTMLGlob("templates/*")

	// **主页路由**
	r.GET("/", handleRequest)

	// **启动后台定时扫描任务**
	go updateCache()

	// **启动服务器**
	r.Run(":80")
}
