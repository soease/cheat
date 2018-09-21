//
// 源于cheat.sh（https://github.com/chubin/cheat.sh），这是Golang中文版。
// 代码改编自github.com/dufferzafar/cheat
// Ease  2018.9.20
//

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	simplejson "github.com/bitly/go-simplejson"
	"github.com/mattn/go-colorable"
	"github.com/mholt/archiver"
	"github.com/russross/blackfriday"
	"github.com/urfave/cli"
)

const (
	version                 string = "0.4"
	show_cli_with_style     int    = 1
	show_text_without_style int    = 2
	show_web_style          int    = 3
)

var (
	stdout   = colorable.NewColorableStdout()
	Language = ""
	config   *JSONData
	LangStr  map[string]string = make(map[string]string)

	url_code2Token = "https://api.weixin.qq.com/sns/jscode2session"
)

type JSONData struct {
	Highlight bool     `json:"highlight"`
	Linewrap  int      `json:"linewrap"`
	Editor    string   `json:"editor"`
	Cheatdirs []string `json:"cheatdirs"`
	Language  string   `json:"language"`
	WebPort   string   `json:"webport"`
	AppID     string   `json:"appid"`
	AppSecret string   `json:"appsecret"`
	Token     string   `json:"token"`
	Encoding  string   `json:"encoding"`
}

var defaults = `{
    "highlight": true,
    "linewrap": 79,
    "cheatdirs": [
        "cheatsheets"
    ],
    "editor": "vim",
    "language": "zh",
    "webport": "8000",
	"AppID": "",
	"AppSecret": "",
	"Token": "",
	"EncodingAESKey": "",       
}`

var AppHelpTemplate = `
{{.Name}} - {{.Usage}}

%s: {{.Author}}
%s: {{.Version}}
%s: {{.Compiled}}
%s: {{.Email}}

%s: {{.Name}} <command> [cheatsheet] [<args>]

%s:
    {{range .Commands}}{{.Name}}{{with .ShortName}}, {{.}}{{end}}{{ "\t" }}{{.Usage}}
    {{end}}

%s:
    {{.Name}} show git              %s
    {{.Name}} show git -copy 12     %s
    {{.Name}} edit at               %s

`

func main() {
	config = &JSONData{}
	config.ReadConfig()
	Language = config.Language //优先适配的语言
	debug(config)

	LoadLang() // 截入语言

	app := cli.NewApp()
	cli.AppHelpTemplate = fmt.Sprintf(AppHelpTemplate, SelLang("Author"), SelLang("Version"),
		SelLang("Date"), SelLang("Email"), SelLang("Usage"), SelLang("Commands"), SelLang("Examples"),
		SelLang("Shows git cheatsheet"), SelLang("Copy the 12th git cheat"), SelLang("Edit cheatsheet named at"))

	app.Name = "cheat"
	app.Usage = SelLang("Create and view command-line cheatsheets")
	app.Version = version
	app.Author = "Ease"
	app.Compiled, _ = time.Parse("2006-01-02", "2018-09-07")
	app.Email = "scwy@qq.com"

	cli.HelpFlag = cli.BoolFlag{
		Name:  "help,h",
		Usage: SelLang("show help"),
	}
	cli.VersionFlag = cli.BoolFlag{
		Name:  "version,v",
		Usage: SelLang("print the version"),
	}
	app.Commands = []cli.Command{
		{
			Name:    "show",
			Default: true,
			Aliases: []string{"s"},
			Usage:   SelLang("Show cheats related to a command"),
			Flags: []cli.Flag{
				cli.IntFlag{
					Name:  "copy",
					Usage: SelLang("cheat number to copy"),
				},
			},

			Action: func(c *cli.Context) {
				ret, filename := allCheats(c.Args().First(), show_cli_with_style)
				if ret == "" { //不存在小抄
					fmt.Fprintf(os.Stderr, fmt.Sprintf(SelLang("No cheatsheet found for '%s'")+"\n", c.Args().First()))
					fmt.Fprintf(os.Stderr, fmt.Sprintf(SelLang("To create a new sheet, run: cheat edit %s")+"\n", c.Args().First()))
					os.Exit(1)
				} else {
					if c.Int("copy") != 0 { //复制小抄
						copyCheat(filename, c.Args().First(), c.Int("copy"))
					} else { //输出小抄
						fmt.Fprintln(stdout, ret)
					}
				}
			},
		},
		{
			Name:    "edit",
			Aliases: []string{"e"},
			Usage:   SelLang("Add/Edit a cheat"),
			Action: func(c *cli.Context) {
				var cheatfile = filepath.Join(config.Cheatdirs[0], c.Args().First())
				editCheat(cheatfile, config.Editor)
			},
		},
		{
			Name:    "find",
			Aliases: []string{"f"},
			Usage:   SelLang("find all cheat"),
			Action: func(c *cli.Context) {
				fmt.Println(WalkDir(config.Cheatdirs[0], c.Args().First()))
			},
		},
		{
			Name:    "web",
			Aliases: []string{"w"},
			Usage:   SelLang("Start a web server"),
			Action: func(c *cli.Context) {
				log.Printf(SelLang("Start a web server, Port %s ...")+"\n", config.WebPort)

				http.HandleFunc("/", IndexHandler)

				err := http.ListenAndServe(":"+config.WebPort, nil)
				fmt.Println(err)
			},
		},
		{
			Name:    "list",
			Aliases: []string{"l"},
			Usage:   SelLang("List all available cheats"),
			Action: func(c *cli.Context) {
				path := config.Cheatdirs[0]
				if len(c.Args()) > 0 {
					path = path + "/" + c.Args()[0]
				}
				fmt.Println(listCheat(path, false))
			},
		},
		{
			Name:    "config",
			Aliases: []string{"c"},
			Usage:   SelLang("Edit the config file"),
			Action: func(c *cli.Context) {
				rcfile := filepath.Join(".cheatrc")
				editCheat(rcfile, config.Editor)
			},
		},
		{
			Name:  "fetch",
			Usage: SelLang("Fetch cheats from Github"),
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "dir, d",
					Value: config.Cheatdirs[0],
					Usage: SelLang("cheats directory"),
				},
				cli.StringFlag{
					Name:  "repo, r",
					Value: "https://github.com/chrisallenlane/cheat/cheat/cheatsheets",
					Usage: SelLang("repository to fetch cheats from"),
				},
				cli.StringFlag{
					Name:  "local, l",
					Usage: SelLang("local path to store repository"),
				},
			},
			Action: func(c *cli.Context) {
				if c.String("local") == "" && os.Getenv("GOPATH") == "" {
					fmt.Fprintf(os.Stderr, SelLang("Local path to store repo is required")+".\n")
					return
				}

				fetchCheats(c)
			},
		},
		{
			Name:  "help",
			Usage: SelLang("Shows a list of commands or help for one command"),
			Action: func(c *cli.Context) error {
				args := c.Args()
				if args.Present() {
					return cli.ShowCommandHelp(c, args.First())
				}

				cli.ShowAppHelp(c)
				return nil
			},
		},
	}

	app.Run(os.Args)
}

// 复制小抄
func copyCheat(cheatfile string, cmdname string, cheatno int) {
	file, _ := os.Open(cheatfile)
	scanner := bufio.NewScanner(file)

	var i = 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(line, cmdname) {
			i++
		}

		if cheatno == i {
			re := regexp.MustCompile(`([^#]*)`)
			res := re.FindAllStringSubmatch(line, -1)
			line = strings.TrimSpace(res[0][0])
			clipboard.WriteAll(line)
			fmt.Fprintln(stdout, fmt.Sprintf(SelLang("%s Copied to Clipboard: %s %s"), "\x1b[32;5m", "\x1b[0m", line))
			break
		}
	}
	file.Close()
}

// 编辑小抄
func editCheat(cheatfile string, configEditor string) {
	editor, err := exec.LookPath(configEditor)

	if err != nil {
		fmt.Fprintf(os.Stderr, SelLang("Editor not found")+": "+editor)
	}

	cmd := exec.Command(editor, cheatfile)
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	cmd.Run()
}

// 获取小抄
func fetchCheats(c *cli.Context) {
	// parse repo url
	repo, err := url.Parse(c.String("repo"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}

	repoPath := strings.Split(repo.Path, "/")
	if len(repoPath) <= 3 {
		fmt.Fprintln(os.Stderr, SelLang("Invalid Repo URL"))
		return
	}

	cheatsPath := repoPath[3:]
	repo.Path = fmt.Sprintf("/%s/%s", repoPath[1], repoPath[2])

	// directory where the cloned repository is stored
	var cloneDir string
	if c.String("local") != "" {
		cloneDir = c.String("local")
	} else if os.Getenv("GOPATH") != "" {
		cloneDir = filepath.Join(os.Getenv("GOPATH"), "src", repo.Host, repoPath[1], repoPath[2])
	}

	// update the repo
	updated, err := updateLocalRepo(repo.String(), cloneDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}

	// copy cheats
	if updated {
		srcPath := cloneDir
		for _, p := range cheatsPath {
			srcPath = filepath.Join(srcPath, p)
		}

		count, err := copyCheatFiles(srcPath, c.String("dir"))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		fmt.Fprintf(os.Stderr, fmt.Sprintf(SelLang("%s cheats updated")+"\n", strconv.Itoa(count)))
	} else {
		fmt.Fprintf(os.Stderr, SelLang("No cheats updated")+"\n")
	}
}

// 更新本地库
func updateLocalRepo(url, dir string) (bool, error) {
	var cmd *exec.Cmd

	_, err := os.Stat(dir)
	if os.IsNotExist(err) {
		cmd = exec.Command("git", "clone", url, dir)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return true, cmd.Run()
	} else {
		cmd = exec.Command("git", "pull", url)
		cmd.Dir = dir

		out, err := cmd.CombinedOutput()
		if err != nil {
			return false, err
		}

		res := string(out)
		fmt.Fprint(os.Stderr, res)

		updated := true
		if strings.Contains(res, SelLang("Already up-to-date")) {
			updated = false
		}

		return updated, nil
	}
}

// 复制小抄文件
func copyCheatFiles(cloneDir, cheatsDir string) (int, error) {
	fmt.Fprintf(os.Stderr, fmt.Sprintf(SelLang("Copying from %s to %s")+"\n", cloneDir, cheatsDir))
	count := 0

	files, err := ioutil.ReadDir(cloneDir)
	if err != nil {
		return count, err
	}

	err = os.MkdirAll(cheatsDir, 0755)
	if err != nil {
		return count, err
	}

	for _, f := range files {
		count += 1

		err := copyFile(filepath.Join(cloneDir, f.Name()), filepath.Join(cheatsDir, f.Name()))
		if err != nil {
			return count, err
		}
	}

	return count, nil
}

// 复制文件
func copyFile(src, dst string) error {
	s, err := os.Open(src)
	if err != nil {
		return err
	}
	defer s.Close()

	d, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer d.Close()

	if _, err := io.Copy(d, s); err != nil {
		return err
	}

	return nil
}

// 判断文件夹是否存在
func PathExists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	return false
}

func listCheat(path string, web bool) (ret string) {
	var newline string
	if web {
		newline = "<br/>"
	} else {
		newline = "\n"
	}
	files, _ := ioutil.ReadDir(path)
	for _, f := range files {
		if strings.HasPrefix(f.Name(), ".") == false {
			ret = ret + newline + f.Name()
		}
	}
	return ret
}

// ---- Web服务 ---------------------------------------------------------
func IndexHandler(w http.ResponseWriter, r *http.Request) {
	var web bool //是否为web访问
	var Lang string
	var ret string

	//根据Web请求的头文件判断是否为浏览器查看，便于确定返回格式
	if len(r.Header["Accept-Language"]) == 0 {
		web = false
	} else {
		web = true
	}

	// 根据浏览器，判断默认语言
	if web {
		Lang = strings.Split(r.Header["Accept-Language"][0], ";")[0]
		switch strings.ToUpper(strings.Split(Lang, ",")[0]) {
		case "ZH-CN":
			Lang = "zh"
		default:
			Lang = ""
		}
	} else {
		Lang = Language
	}

	var cmdname = r.URL.Path[1:len(r.URL.Path)] //取消Web请求中的第一个划线符号
	if r.URL.Path == "/" {                      //未进行查询则显示说明文件
		cmdname = ".readme.md"
	} else if cmdname == "favicon.ico" { //网站访问取图标，不关注
		return
	} else if strings.HasPrefix(cmdname, "pic/") { //访问图片
		var staticfs = http.FileServer(http.Dir("."))
		staticfs.ServeHTTP(w, r)
		return
	}

	if len(r.Form["flag"]) > 0 {
		if r.Form["flag"][0] == "wx" { //微信访问
			web = false
		}
	}
	debug(fmt.Sprintf("url: %s, key: %s, web: %t", r.URL.Path, cmdname, web))
	debug(r.Header)

	//网站功能
	if strings.HasPrefix(cmdname, "down") { //打包下载通用小抄
		zipfile := zipCheat(config.Cheatdirs[0])
		w.Header().Add("Content-Disposition", fmt.Sprintf("attachment; filename=%s", zipfile))
		http.ServeFile(w, r, zipfile)
		return
	} else if strings.HasPrefix(cmdname, "list") { //小抄列表
		debug(len(strings.Split(cmdname, "/")))
		if len(strings.Split(cmdname, "/")) == 1 { //二级目录列表
			ret = listCheat(config.Cheatdirs[0], web)
		} else {
			ret = listCheat(filepath.Join(config.Cheatdirs[0], strings.Split(cmdname, "/")[1]), web)
		}
	} else if strings.HasPrefix(cmdname, "pdown") { //私人小抄下载
		return
	} else if strings.HasPrefix(cmdname, "total") { //访问统计
		return
	} else if strings.HasPrefix(cmdname, "pup") { //私人小抄上传
		return
	} else if strings.HasPrefix(cmdname, "login") { //登陆，返回说明信息
		cmdname = ".weixin"
		ret, _ = allCheats(cmdname, show_text_without_style)
	} else if strings.HasPrefix(cmdname, "book") { //留言功能

	} else if cmdname == "search" {
		r.ParseForm()
		ret, _ = allCheats(r.Form["key"][0], show_text_without_style)
		cmdname = cmdname + ": " + r.Form["key"][0]
	} else {
		if web {
			ret, _ = allCheats(cmdname, show_web_style)
		} else {
			ret, _ = allCheats(cmdname, show_cli_with_style)
		}

	}
	if web {
		ret = "<html>" + ret + "</html>"
	}
	fmt.Fprintln(w, ret)
	log.Printf("[%5d] %s", len(ret), cmdname)
}

// 从微信服务器获取用户openid--------------------------------

func WeiXinUserInfo(code string) string {
	para := make(map[string]string)

	para["appid"] = config.AppID
	para["secret"] = config.AppSecret
	para["js_code"] = code
	para["grant_type"] = "authorization_code"

	res := httpGet(url_code2Token, para)
	if res == nil {
		return ""
	}
	json, err := simplejson.NewJson(res)
	debug(json)
	if err != nil {
		return ""
	}
	js, _ := json.Map()
	debug(js["openid"])
	return UserLogin(js["openid"].(string))
}

func UserLogin(openid string) string {
	switch openid {
	case "oWu_b4u-aSqSAUbXRiS8w7ZCL0GA":
		return "Ease"
	case "oWu_b4jJirFk9LUaNFvG2qGq1oLU":
		return "wyyyh"
	}
	return ""
}

// --------------------------------------------------------------------

func allCheats(key string, text_type int) (string, string) {
	//var cheatfile = filepath.Join(config.Cheatdirs[0], key)

	var filename map[int]string = make(map[int]string)

	// 添加语言判断
	k := strings.Split(key, "/")

	if len(k) == 2 { //包含指定二级目录
		filename[0] = filepath.Join(config.Cheatdirs[0], k[0], Language+"_"+k[1])
		filename[1] = filepath.Join(config.Cheatdirs[0], key)
	} else {
		filename[0] = filepath.Join(config.Cheatdirs[0], k[0], Language+"_"+k[0])
		filename[1] = filepath.Join(config.Cheatdirs[0], Language+"_"+key)
		filename[2] = filepath.Join(config.Cheatdirs[0], k[0], k[0]) //可能存在二级目录的小抄，但查询并未给出目录
		filename[3] = filepath.Join(config.Cheatdirs[0], key)
	}

	debug(filename)
	for _, n := range filename {
		if PathExists(n) { //满足一个即退出
			return showCheats(n, text_type), n
		}
	}
	return "", ""
}

// 显示小抄
func showCheats(cheatfile string, text_type int) (ret string) {
	var formatStr1, formatStr2, newline, no string

	MarkDown := strings.HasSuffix(cheatfile, ".md")

	if text_type == show_web_style { //网页样式
		newline = "<br/>"
		if MarkDown { //md扩展名认为是MarkDown文件格式
			formatStr1 = "%s %s"
			formatStr2 = "%s%s %s"
		} else {
			formatStr1 = "%s<b>%s</b>" + newline
			formatStr2 = "%s%s %s" + newline
		}
	} else if text_type == show_cli_with_style { //命令行显示模式
		newline = "\n"
		formatStr1 = "%s\x1b[33;1m%s\x1b[0m " + newline
		formatStr2 = "%s\x1b[36;1m(%s)\x1b[0m %s " + newline
	} else { //纯文本模式
		newline = "\n"
		formatStr1 = "%s%s " + newline
		formatStr2 = "%s(%s) %s " + newline
	}

	file, _ := os.Open(cheatfile)
	scanner := bufio.NewScanner(file)
	defer file.Close()

	var i = 1
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") {
			ret = fmt.Sprintf(formatStr1, ret, line)
		} else if strings.HasPrefix(line, ".") {
			ret = fmt.Sprintf(formatStr1, ret, line[1:len(line)])
		} else if len(line) > 0 {
			if MarkDown == false { //MarkDown文件格式不需要编号
				no = strconv.Itoa(i)
			}
			ret = fmt.Sprintf(formatStr2, ret, no, line)
			i++
		} else {
			ret = ret + newline + line
		}
	}

	if MarkDown { //md扩展名认为是MarkDown文件格式
		ret = string(blackfriday.MarkdownBasic([]byte(ret)))
	}
	return ret
}

// 载入语言
func LoadLang() {
	if PathExists(Language + ".lng") {
		file, _ := os.Open(Language + ".lng")
		scanner := bufio.NewScanner(file)

		for scanner.Scan() {
			line := strings.Split(strings.TrimSpace(scanner.Text()), "=")
			if len(line) > 0 {
				LangStr[strings.TrimSpace(line[0])] = strings.TrimSpace(line[1])
			}
		}
	}
}

// 语言输入
func SelLang(en string) string {
	if LangStr[en] == "" {
		LangStr[en] = en
	}
	return LangStr[en]
}

//获取指定目录及所有子目录下的所有文件，可以匹配后缀过滤。
func WalkDir(dirPth, suffix string) (files []string, err error) {
	files = make([]string, 0, 30)
	suffix = strings.ToUpper(suffix)                                                     //忽略后缀匹配的大小写
	err = filepath.Walk(dirPth, func(filename string, fi os.FileInfo, err error) error { //遍历目录
		//if err != nil { //忽略错误
		// return err
		//}
		if fi.IsDir() { // 忽略目录
			return nil
		}
		if strings.HasSuffix(strings.ToUpper(fi.Name()), suffix) {
			files = append(files, filename)
		}
		return nil
	})
	return files, err
}

func zipCheat(path string) (filename string) {
	filename = "cheatsheets.zip"
	archiver.Zip.Make(filename, []string{"cheatsheets"})
	return
}

//
func httpGet(url string, para map[string]string) []byte {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil
	}

	q := req.URL.Query()
	for i, n := range para {
		q.Add(i, n)
	}
	req.URL.RawQuery = q.Encode()
	debug(req.URL.String())

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)
	return body
}

// 调试时才输出的信息
func debug(info interface{}) {
	if strings.ToUpper(os.Getenv("EASEDEBUG")) == "TRUE" {
		fmt.Println(info)
	}
}

func (q *JSONData) ReadConfig() error {
	var rcfile string
	usr, _ := user.Current()
	if strings.HasPrefix(ExecPath(), "/tmp/") { //解决调试时的问题，硬性指定了目录
		rcfile = "/home/ease/go/my/src/github.com/soease/cheat/.cheatrc"
	} else {
		rcfile = filepath.Join(ExecPath(), ".cheatrc")
	}

	settings := []byte(defaults)
	if _, err := os.Stat(rcfile); os.IsNotExist(err) {
		ioutil.WriteFile(rcfile, []byte(defaults), 0777)
	} else {
		settings, _ = ioutil.ReadFile(rcfile)
	}

	//Umarshalling JSON into struct
	var data = &q
	err := json.Unmarshal(settings, data)
	if err != nil {
		return err
	}
	for i, dir := range q.Cheatdirs {
		if strings.HasPrefix(dir, "~/") {
			q.Cheatdirs[i] = filepath.Join(usr.HomeDir, dir[2:])
		}
	}
	return nil
}

func ExecPath() string {
	execPath, err := exec.LookPath(os.Args[0])
	if err != nil {
		return ""
	}
	//    Is Symlink
	fi, err := os.Lstat(execPath)
	if err != nil {
		return ""
	}
	if fi.Mode()&os.ModeSymlink == os.ModeSymlink {
		execPath, err = os.Readlink(execPath)
		if err != nil {
			return ""
		}
	}
	execDir := filepath.Dir(execPath)
	if execDir == "." {
		execDir, err = os.Getwd()
		if err != nil {
			return ""
		}
	}
	return execDir
}
