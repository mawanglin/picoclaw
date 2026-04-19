@echo off
chcp 65001 >nul
color 0A
echo ==============================================
echo        Git 批量拉取 + 多远端推送脚本
echo ==============================================
echo.

cd /d "%~dp0"

:: ==============================================
:: 第一步：显示所有远端
:: ==============================================
echo 【1】当前仓库所有远端列表：
echo.
git remote -v
echo.

set /p "fetchRemote=请输入【要拉取所有分支】的远端名称："
echo.

:: ==============================================
:: 第二步：拉取远端所有分支 + 批量创建本地分支
:: ==============================================
echo 正在拉取远端 [%fetchRemote%] ...
git fetch %fetchRemote%
echo.

echo 正在批量创建本地分支...
for /f "delims=" %%i in ('git branch -r ^| findstr "%fetchRemote%/" ^| findstr /v "HEAD"') do (
    set "branch=%%i"
    setlocal enabledelayedexpansion
    set "localBranch=!branch:%fetchRemote%/=!"
    git branch !localBranch! !branch! 2>nul
    echo 已创建分支：!localBranch!
    endlocal
)
echo.
echo ✅ 拉取 & 创建本地分支完成！
echo ------------------------------------------------
echo.

:: ==============================================
:: 第三步：再次显示远端，准备推送
:: ==============================================
echo 【2】当前所有远端：
echo.
git remote -v
echo.

set /p "pushRemotes=请输入【要推送的远端】(多个用空格分隔，例如：gitee gitlab)："
echo.

:: ==============================================
:: 第四步：循环推送到多个远端
:: ==============================================
echo 开始推送所有本地分支到多个远端...
echo.

for %%i in (%pushRemotes%) do (
    echo ======================================
    echo 正在推送到远端：%%i
    echo ======================================
    git push %%i --all
    echo.
)

echo ==============================================
echo              🎉 所有推送任务完成！
echo ==============================================
pause >nul