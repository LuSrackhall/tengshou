// Command tengshou-cli is the headless entry point: it seeds the demo data,
// renders contracts, runs the local HTTP service and creates backups.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/LuSrackhall/tengshou/internal/app"
	"github.com/LuSrackhall/tengshou/internal/demo"
	"github.com/LuSrackhall/tengshou/internal/httpapi"
	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/pdfdoc"
	"github.com/LuSrackhall/tengshou/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "错误："+err.Error())
		os.Exit(1)
	}
}

type commonFlags struct {
	dataDir   *string
	fontPath  *string
	fontIndex *int
}

func newFlags(name string) (*flag.FlagSet, *commonFlags) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	c := &commonFlags{
		dataDir:   fs.String("data-dir", "", "数据目录（默认使用系统应用数据目录）"),
		fontPath:  fs.String("font", "", "中文字体文件路径（覆盖配置）"),
		fontIndex: fs.Int("font-index", 0, "字体集合（.ttc）内的字体序号"),
	}
	return fs, c
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return errors.New("缺少子命令")
	}
	cmd := args[0]
	switch cmd {
	case "version", "-version", "--version":
		fmt.Println("腾手", version.String())
		return nil
	case "help", "-h", "--help":
		usage()
		return nil
	case "demo":
		fs, c := newFlags("demo")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return runDemo(*c.dataDir, *c.fontPath, *c.fontIndex)
	case "serve":
		fs, c := newFlags("serve")
		port := fs.Int("port", 0, "监听端口（0 表示使用配置）")
		loopback := fs.Bool("loopback", false, "只绑定回环地址")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return runServe(*c.dataDir, *c.fontPath, *c.fontIndex, *port, *loopback)
	case "render":
		fs, c := newFlags("render")
		contractID := fs.String("contract", "", "合同 id（必填）")
		templateID := fs.String("template", "", "模板 id（默认使用合同关联模板）")
		out := fs.String("out", "", "输出 PDF 路径")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return runRender(*c.dataDir, *c.fontPath, *c.fontIndex, *contractID, *templateID, *out)
	case "backup":
		fs, c := newFlags("backup")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return runBackup(*c.dataDir)
	default:
		usage()
		return fmt.Errorf("未知子命令 %q", cmd)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `腾手（tengshou）%s

用法：
  tengshou-cli [全局参数] <子命令> [参数]

子命令：
  demo      生成示例合同、签名与 PDF（可重复执行）
  serve     启动本地 HTTP 服务（含局域网测试页）
  render    用模板重新生成一份合同 PDF
  backup    备份 SQLite 数据库到 backups/
  version   显示版本

全局参数：
  --data-dir <路径>   数据目录
  --font <路径>       中文字体文件
  --font-index <n>    .ttc 字体集合序号

示例：
  tengshou-cli demo --data-dir ./data
  tengshou-cli serve --data-dir ./data --port 8765
  tengshou-cli render --data-dir ./data --contract demo-contract-1
`, version.String())
}

func openApp(dataDir, fontPath string, fontIndex int) (*app.App, error) {
	a, err := app.Open(dataDir)
	if err != nil {
		return nil, err
	}
	if fontPath != "" {
		abs, err := filepath.Abs(fontPath)
		if err != nil {
			a.Close()
			return nil, err
		}
		a.Config.PDF.FontPath = abs
	}
	if fontIndex > 0 {
		a.Config.PDF.FontIndex = fontIndex
	}
	return a, nil
}

func runDemo(dataDir, fontPath string, fontIndex int) error {
	a, err := openApp(dataDir, fontPath, fontIndex)
	if err != nil {
		return err
	}
	defer a.Close()
	ctx := context.Background()
	res, err := demo.Run(ctx, a, time.Now())
	if err != nil {
		return err
	}
	fmt.Println("示例数据已生成（字段均通过产品「新建字段」功能创建）：")
	fmt.Printf("  数据目录      %s\n", a.Paths.Root)
	fmt.Printf("  字段          %d 个\n", len(res.Fields))
	fmt.Printf("  合同          %s（%s，%d 年度）\n", res.Contract.Title, res.Contract.ContractNo, res.Contract.Year)
	fmt.Printf("  历史合同      %s（%s，%d 年度）\n", res.HistoricalContract.Title,
		res.HistoricalContract.ContractNo, res.HistoricalContract.Year)
	fmt.Printf("  甲方          %s\n", res.PartyA.Name)
	fmt.Printf("  乙方          %s\n", res.PartyB.Name)
	fmt.Printf("  模板          %s v%d\n", res.Template.Name, res.Template.Version)
	fmt.Printf("  签名图片      %s\n", res.Signature.AssetPath)
	fmt.Printf("  公章图片      %s\n", res.Seal.AssetPath)
	fmt.Printf("  PDF           %s\n", res.PDFPath)
	fmt.Printf("  PDF 页数      %d\n", res.Document.PageCount)
	fmt.Printf("  字体          %s\n", res.FontPath)
	fmt.Println("  台账界面      tengshou-cli serve 后打开 /app/")
	return nil
}

func runServe(dataDir, fontPath string, fontIndex, port int, loopback bool) error {
	a, err := openApp(dataDir, fontPath, fontIndex)
	if err != nil {
		return err
	}
	defer a.Close()
	cfg := a.Config
	if loopback {
		cfg.HTTP.AllowLAN = false
		cfg.HTTP.Bind = "127.0.0.1"
	}
	if port > 0 {
		cfg.HTTP.Port = port
	}
	server := httpapi.New(httpapi.Options{
		Reader:  a.Reader,
		Service: a.Service,
		Logger:  a.Logger,
		Config:  cfg,
		Version: version.String(),
	})
	ln, err := server.Listen()
	if err != nil {
		return fmt.Errorf("监听失败: %w", err)
	}
	fmt.Println("本地服务已启动：")
	for _, u := range server.URLs(ln) {
		fmt.Printf("  %s\n", u)
	}
	if cfg.HTTP.Token != "" {
		fmt.Printf("  数据接口令牌：%s\n", cfg.HTTP.Token)
	}
	fmt.Println("按 Ctrl+C 停止。")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return server.Serve(ctx, ln)
}

func runRender(dataDir, fontPath string, fontIndex int, contractID, templateID, out string) error {
	if strings.TrimSpace(contractID) == "" {
		return errors.New("render 需要 --contract")
	}
	a, err := openApp(dataDir, fontPath, fontIndex)
	if err != nil {
		return err
	}
	defer a.Close()
	gen, err := a.Generator()
	if err != nil {
		return err
	}
	ctx := context.Background()
	detail, err := a.Reader.GetContractDetail(ctx, contractID)
	if err != nil {
		return fmt.Errorf("读取合同: %w", err)
	}
	tid := templateID
	if tid == "" {
		tid = detail.Contract.TemplateID
	}
	if tid == "" {
		return errors.New("合同未关联模板，请用 --template 指定")
	}
	tpl, err := a.Reader.GetTemplate(ctx, tid)
	if err != nil {
		return fmt.Errorf("读取模板: %w", err)
	}
	outPath := out
	if outPath == "" {
		outPath = filepath.Join(a.Paths.Outputs,
			fmt.Sprintf("%s-%s.pdf", detail.Contract.ID, time.Now().Format("20060102-150405")))
	}
	outPath, err = filepath.Abs(outPath)
	if err != nil {
		return err
	}
	seals, contractSeals, err := a.Service.SealsForContract(ctx, detail.Contract)
	if err != nil {
		return fmt.Errorf("读取公章图片: %w", err)
	}
	data := pdfdoc.TemplateData{
		Contract:      detail.Contract,
		PartyA:        detail.PartyA,
		PartyB:        detail.PartyB,
		Signatures:    detail.Signatures,
		Seals:         seals,
		ContractSeals: contractSeals,
		Data:          detail.Contract.Data,
		Now:           time.Now(),
	}
	res, err := gen.RenderFile(tpl.HTML, data, a.Paths.Root, detail.Contract.Title, outPath)
	if err != nil {
		return err
	}
	rel, _ := filepath.Rel(a.Paths.Root, outPath)
	docID, _ := model.NewID("doc")
	document := model.OutputDocument{
		ID:              docID,
		ContractID:      detail.Contract.ID,
		TemplateID:      tpl.ID,
		TemplateVersion: tpl.Version,
		FilePath:        filepath.ToSlash(rel),
		SHA256:          res.SHA256,
		PageCount:       res.PageCount,
		CreatedAt:       model.Now(),
	}
	if err := a.Store.PutOutputDocument(ctx, &document); err != nil {
		return err
	}
	fmt.Printf("PDF 已生成：%s（%d 页，sha256 %s…）\n", outPath, res.PageCount, res.SHA256[:12])
	if len(res.Missing) > 0 {
		fmt.Printf("注意：字体缺少字形 %q\n", string(res.Missing))
	}
	return nil
}

func runBackup(dataDir string) error {
	a, err := openApp(dataDir, "", 0)
	if err != nil {
		return err
	}
	defer a.Close()
	path, err := a.Backup()
	if err != nil {
		return err
	}
	fmt.Printf("数据库备份：%s\n", path)
	return nil
}
