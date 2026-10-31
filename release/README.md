# DeskGrid 发布目录

在项目根目录运行 `scripts/package.ps1` 生成发布内容：

- `DeskGrid/`：可直接运行的解压版目录。
- `DeskGrid-windows-amd64.zip`：供用户下载的完整发布包。
- `SHA256SUMS.txt`：ZIP 与 EXE 的 SHA256 校验值。
- `RELEASE_NOTES.md`：可用于 GitHub Release 的发布说明。

GitHub 发布时，先确认版本与实际使用效果，再在仓库 Releases 页面创建版本，填写发布说明，上传 ZIP 和校验文件。生成此文件夹不会自动发布 GitHub Release。

本目录仅此 README 提交源码仓库，其他内容由脚本生成，避免将二进制和第三方许可证副本混入源码提交。
