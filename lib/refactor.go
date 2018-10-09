package lib

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var FSet *token.FileSet
var Imports *ImportsInfo

type ImportsInfo struct {
	PackageMap  map[string]*PackageInfo  // the key is package absolute path
	FileImports map[string][]*FileImport // the key is the filepath of importing the file
}

var regexAbsolutePath = regexp.MustCompile(`^\/`)

var workingDir string

func (i *ImportsInfo) AddImports(file *ast.File) (absPath string) {

	pos := FSet.Position(file.Package)
	absPath = pos.Filename

	if !regexAbsolutePath.MatchString(absPath) {
		absPath = path.Join(workingDir, absPath)
	}

	if filepathExist(absPath) {
		if _, ok := i.FileImports[absPath]; !ok {
			i.FileImports[absPath] = []*FileImport{}
		}
	} else {
		fmt.Println("Import file path not exist", absPath)
		return ""
	}

	for _, im := range file.Imports {

		pkgPath := findPkgPath(im.Path.Value)

		if len(pkgPath) != 0 {
			alias := ""
			if im.Name != nil {
				alias = im.Name.Name
			}
			fImport := &FileImport{Alias: alias, ImportSpec: im}
			fImport.PackageInfo = newPackageInfo(pkgPath)
			i.FileImports[absPath] = append(i.FileImports[absPath], fImport)
			i.PackageMap[pkgPath] = fImport.PackageInfo
		}
	}
	return absPath
}

const gopath string = "/opt/kingsoft/src"

func findPkgPath(rPath string) string {
	result := strings.Replace(rPath, "\"", "", -1)
	pkgPath := filepath.Join(gopath, result)

	if _, err := os.Stat(pkgPath); os.IsNotExist(err) {
		return ""
	}

	return pkgPath

}

type FileImport struct {
	Alias       string
	ImportSpec  *ast.ImportSpec
	PackageInfo *PackageInfo
}

const (
	stAstFileFailedParseFile string = "failedParseFile"
	stAstFileParsedFile      string = "parsedFile"
)

type AstFile struct {
	Status   string
	Filename string
	File     *ast.File
	Error    error
}

const (
	stPackageInfoPackageDirNotExist     string = "packageDirNotExist"
	stPackageInfoFailedLoopPkgDirectory string = "failedLoopPkgDirectory"
	stPackageInfoImported               string = "stImported"
)

// PackageInfo Describe a imported package info
type PackageInfo struct {
	PkgName  string
	PkgPath  string
	AstFiles []*AstFile
	Status   string
}

var regexpTestGoFile = regexp.MustCompile("_test.go$")

// ParseFiles Parsing a package included files
func (p *PackageInfo) ParseFiles() {
	if _, err := os.Stat(p.PkgPath); os.IsNotExist(err) {
		p.Status = stPackageInfoPackageDirNotExist
		return
	}
	if p.AstFiles == nil {
		p.AstFiles = []*AstFile{}
	}
	if files, err := filepath.Glob(filepath.Join(p.PkgPath, "*.go")); err == nil {
		for _, filename := range files {
			if regexpTestGoFile.MatchString(filename) {
				continue
			}
			var astFile *AstFile
			if parsedFile, err := parser.ParseFile(FSet, filename, nil, parser.ParseComments); err == nil {
				astFile = &AstFile{File: parsedFile, Status: stAstFileParsedFile, Filename: filename}
				if len(p.PkgName) == 0 {
					p.PkgName = parsedFile.Name.Name
				} else if p.PkgName != parsedFile.Name.Name {
					fmt.Printf("Find a package's name is conflict: PkgPath: %s,PkgName %s | filename: %s, filePackageName: %s \n", p.PkgPath, p.PkgName, filename, parsedFile.Name.Name)
				}
			} else {
				astFile = &AstFile{Status: stAstFileFailedParseFile, Filename: filename, Error: err}
			}

			p.AstFiles = append(p.AstFiles, astFile)
		}
	} else {
		p.Status = stPackageInfoFailedLoopPkgDirectory
		return
	}
	p.Status = stPackageInfoImported
}

func newPackageInfo(pkgPath string) *PackageInfo {
	pkgInfo := &PackageInfo{PkgPath: pkgPath}
	pkgInfo.ParseFiles()
	return pkgInfo
}

// FunDec Describe a function declaration
type FunDec struct {
	Depth      int
	FunDecl    *ast.FuncDecl
	PkgName    string
	StructName string
	FunCalls   []*FunCall // 这个函数声明里调用的别的函数
	CalledFuns []*FunCall // 这个函数声明的函数调用来源
	AstFile    *ast.File
	AbsPath    string
	DecCode    string
}

const (
	stFunCallProceed string = "stFunCallProceed"
	stFunCallIgnored string = "stFunCallIgnored"
)

// FunCall Describe a function call
type FunCall struct {
	Depth            int
	FileNode         *ast.File
	ParentFunCallDec *FunDec // 调用这个函数的函数的声明
	FunDecs          []*FunDec
	CallExpr         *ast.CallExpr
	CallCode         string
	State            string
}

var regexpFunctionName = regexp.MustCompile("\\( *$")

// RefactorFun refactor enter function
func RefactorFun(fileName, funName string) *FunDec {

	pwd, err := os.Getwd()
	if err != nil {
		panic("Could not get working directory.")
	}
	workingDir = pwd

	Imports = &ImportsInfo{}
	Imports.PackageMap = make(map[string]*PackageInfo)
	Imports.FileImports = make(map[string][]*FileImport)
	FSet = token.NewFileSet()

	astFile, err := parser.ParseFile(FSet, fileName, nil, parser.ParseComments)
	if err != nil {
		fmt.Println("Failed to ParseFile:", err)
		os.Exit(1)
	}

	if !regexpFunctionName.MatchString(funName) {
		funName = fmt.Sprintf("%s%s", funName, "(")
	}

	fdList := findFunDecs(astFile, funName, 0)

	if len(fdList) > 1 {
		fmt.Println("Found more than one function which name is ", funName)
		os.Exit(1)
	}
	fdList[0].ProcessFunCall()

	printStack(fdList[0])
	initCtxAst()
	fmt.Println("---------------------------")
	chnageCode(fdList[0])
	writeBack(fdList[0])

	return fdList[0]
}
func printStack(fd *FunDec) {

	fmt.Printf("%sFunDecl: %s : %s\n", generateIndent(fd.Depth), fd.AbsPath, fd.DecCode)
	for _, funCall := range fd.FunCalls {
		if funCall.State == stFunCallProceed {
			if len(funCall.FunDecs) > 0 {
				fmt.Printf("%sFunCall: %s\n", generateIndent(funCall.Depth), funCall.CallCode)
				for _, fcFDec := range funCall.FunDecs {
					printStack(fcFDec)
				}
			}
		}
	}
}

func chnageCode(fd *FunDec) {

	if fd.Depth != 0 {
		newParams := []*ast.Field{ctxField}
		for _, f := range fd.FunDecl.Type.Params.List {
			for _, i := range f.Names {
				i.NamePos = token.NoPos
			}
			newParams = append(newParams, f)
		}
		fd.FunDecl.Type.Params.List = newParams
		fd.FunDecl.Name.Name = fd.FunDecl.Name.Name + "WithCtx"
		fd.GCode()
	}

	fmt.Printf("%sFunDecl: %s : %s\n", generateIndent(fd.Depth), fd.AbsPath, fd.DecCode)
	for _, funCall := range fd.FunCalls {
		if funCall.State == stFunCallProceed {
			if len(funCall.FunDecs) > 0 {
				ctxVar := &ast.Ident{Name: "ctx"}
				newArgs := []ast.Expr{ctxVar}
				for _, arg := range funCall.CallExpr.Args {
					newArgs = append(newArgs, arg)
				}
				chnageFunCallName(funCall)
				funCall.CallExpr.Args = newArgs
				funCall.GCode()
				fmt.Printf("%sFunCall: %s\n", generateIndent(funCall.Depth), funCall.CallCode)
				for _, fcFDec := range funCall.FunDecs {
					chnageCode(fcFDec)
				}
			}
		}
	}
}

func writeBack(fd *FunDec) {

	var file *os.File
	var err error
	newPath := fd.AbsPath + ".patch"
	if filepathExist(newPath) {
		file, err = os.OpenFile(newPath, os.O_RDWR, 0644)
	} else {
		file, err = os.Create(newPath)
	}

	if err != nil {
		fmt.Println(err)
		panic("write back err")
	}

	conf := &printer.Config{Mode: printer.TabIndent, Tabwidth: 8}
	conf.Fprint(file, FSet, fd.FunDecl)
	file.WriteString("\n\n")
	if fd.Depth != 0 {
		conf.Fprint(file, token.NewFileSet(), makeNewFunDecl(fd.FunDecl))
		file.WriteString("\n\n")
	}
	file.Close()
	for _, funCall := range fd.FunCalls {
		if funCall.State == stFunCallProceed {
			if len(funCall.FunDecs) > 0 {
				for _, fd := range funCall.FunDecs {
					writeBack(fd)
				}
			}
		}
	}
}

func makeNewFunDecl(funDecl *ast.FuncDecl) *ast.FuncDecl {
	n := &ast.FuncDecl{}
	n.Recv = funDecl.Recv
	nList := []*ast.Field{}
	args := []ast.Expr{}

	for i, f := range funDecl.Type.Params.List {
		if i != 0 {
			nList = append(nList, f)
			args = append(args, &ast.Ident{Name: f.Names[0].Name})
		} else {
			args = append(args, &ast.Ident{Name: "context.Background()"})
		}
	}
	n.Type = funDecl.Type
	n.Type.Params.List = nList
	oldName := funDecl.Name.Name
	nName := strings.Replace(funDecl.Name.Name, "WithCtx", "", 1)
	n.Name = &ast.Ident{Name: nName}
	n.Body = funDecl.Body

	recvName := ""
	if funDecl.Recv != nil && funDecl.Recv.List[0] != nil {
		recvName = funDecl.Recv.List[0].Names[0].Name
	}

	stmts := []ast.Stmt{}

	var callWithCtxFun *ast.ReturnStmt
	if len(recvName) > 0 {
		callWithCtxFun = &ast.ReturnStmt{Results: []ast.Expr{
			&ast.CallExpr{
				Fun: &ast.SelectorExpr{
					X:   &ast.Ident{Name: recvName},
					Sel: &ast.Ident{Name: oldName},
				},
				Lparen:   token.NoPos,
				Args:     args,
				Ellipsis: token.NoPos,
				Rparen:   token.NoPos,
			}}}
	} else {
		callWithCtxFun = &ast.ReturnStmt{Results: []ast.Expr{
			&ast.CallExpr{
				Fun:      &ast.Ident{Name: recvName},
				Lparen:   token.NoPos,
				Args:     args,
				Ellipsis: token.NoPos,
				Rparen:   token.NoPos,
			}}}
	}

	stmts = append(stmts, callWithCtxFun)
	n.Body.List = stmts

	return n
}

func chnageFunCallName(fc *FunCall) {
	switch fc.CallExpr.Fun.(type) {
	case *ast.SelectorExpr:
		s := fc.CallExpr.Fun.(*ast.SelectorExpr)
		s.Sel.Name = s.Sel.Name + "WithCtx"
	}
}

const codeCtx string = `package example
func example(ctx context.Context){
}`
const importContext string = "context"

var ctxField *ast.Field

func initCtxAst() {
	codeCtx, err := parser.ParseFile(FSet, "example", codeCtx, parser.ParseComments)
	if err != nil {
		fmt.Println(err)
		panic("Failed parse expression")
	}

	ast.Inspect(codeCtx, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if ok {
			ctxField = fn.Type.Params.List[0]
		}
		return true
	})

}

func generateIndent(depth int) string {
	indent := ""
	tab := " ."
	for i := 0; i < depth; i++ {
		indent += tab
	}
	return indent
}

func findFunDecs(astFile *ast.File, funName string, depth int) []*FunDec {
	list := []*FunDec{}

	ast.Inspect(astFile, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if ok {
			rstr := fmt.Sprintf("\\b%s *\\(", fn.Name.String())
			r := regexp.MustCompile(rstr)
			if r.MatchString(funName) {

				fd := &FunDec{}
				fd.FunCalls = []*FunCall{}
				fd.Depth = depth
				absPath := Imports.AddImports(astFile)
				fd.AstFile = astFile
				fd.AbsPath = absPath
				if astFile.Name != nil {
					fd.PkgName = astFile.Name.Name
				}
				fd.FunDecl = fn
				fd.GCode()
				list = append(list, fd)
			}

		}
		return true
	})

	return list
}

func (fd *FunDec) GCode() {
	var buffer bytes.Buffer
	body := fd.FunDecl.Body
	fd.FunDecl.Body = nil
	format.Node(&buffer, token.NewFileSet(), fd.FunDecl)
	fd.FunDecl.Body = body
	decCode := buffer.String()
	fd.DecCode = decCode
}

func (fd *FunDec) ProcessFunCall() {

	ast.Inspect(fd.FunDecl, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if ok {
			fCall := &FunCall{CallExpr: call, Depth: fd.Depth + 1, ParentFunCallDec: fd}
			fCall.ParentFunCallDec = fd
			fd.FunCalls = append(fd.FunCalls, fCall)
		}
		return true
	})

	for _, funCall := range fd.FunCalls {
		funCall.GCode()
		if ignoreFun(funCall.CallCode) {
			funCall.State = stFunCallIgnored
			continue
		}
		funCall.FunDecs = []*FunDec{}
		funCall.State = stFunCallProceed
		funCall.ProcessFunDec()
	}
}

func (fc *FunCall) GCode() {
	var buffer bytes.Buffer
	format.Node(&buffer, token.NewFileSet(), fc.CallExpr)
	callerCode := buffer.String()
	fc.CallCode = callerCode
}

func (fc *FunCall) ProcessFunDec() {

	// 会死循环
	// if fc.ParentFunCallDec.AstFile != nil {
	// 	funDecs := findFunDecs(fc.ParentFunCallDec.AstFile, fc.CallCode, fc.Depth+1)
	// 	fmt.Println("AAAAA", fc.CallCode)
	// 	fc.FunDecs = append(fc.FunDecs, funDecs...)
	// }

	if fileImportList, ok := Imports.FileImports[fc.ParentFunCallDec.AbsPath]; ok {
		for _, fileImport := range fileImportList {
			for _, astFile := range fileImport.PackageInfo.AstFiles {
				// fmt.Println("!!!", astFile)

				if astFile.File != nil {
					funDecs := findFunDecs(astFile.File, fc.CallCode, fc.Depth+1)
					fc.FunDecs = append(fc.FunDecs, funDecs...)
				}
			}
		}
	} else {
		fmt.Println("CallerCode", fc.CallCode)
		panic("In ProcessFunDec")
	}

	for _, funDec := range fc.FunDecs {
		funDec.ProcessFunCall()
	}
}

func filepathExist(path string) bool {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false
	}

	return true
}
