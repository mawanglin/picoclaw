#!/bin/bash

clear
echo "=============================================="
echo "        Git 批量拉取 + 多远端推送脚本 (Linux)"
echo "=============================================="
echo ""

# 1. 显示所有远端
echo "【1】当前仓库所有远端列表："
echo ""
git remote -v
echo ""
read -p "请输入【要拉取所有分支】的远端名称：" fetchRemote

# 2. 拉取远端所有分支
echo ""
echo "正在拉取远端 [$fetchRemote] 所有分支..."
git fetch "$fetchRemote"

# 3. 批量创建本地分支
echo ""
echo "正在批量创建本地分支..."
git branch -r | grep "$fetchRemote/" | grep -v HEAD | while read remoteBranch; do
    localBranch=${remoteBranch#"$fetchRemote/"}
    git branch "$localBranch" "$remoteBranch" 2>/dev/null
    echo "已创建分支：$localBranch"
done

echo ""
echo "✅ 拉取 & 创建本地分支完成！"
echo "------------------------------------------------"
echo ""

# 4. 再次显示远端
echo "【2】当前所有远端："
echo ""
git remote -v
echo ""
read -p "请输入【要推送的远端】(多个用空格分隔)：" pushRemotes

# 5. 循环推送到多个远端
echo ""
echo "开始推送所有本地分支到多个远端..."
echo ""

for remote in $pushRemotes; do
    echo "======================================"
    echo "正在推送到远端：$remote"
    echo "======================================"
    git push "$remote" --all
    echo ""
done

echo "=============================================="
echo "              🎉 所有任务完成！"
echo "=============================================="