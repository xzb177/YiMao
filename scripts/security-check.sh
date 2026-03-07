#!/bin/bash

# YiMao 代码安全检查脚本
# 用于快速检查代码中的常见安全问题

echo "🔍 YiMao 代码安全检查"
echo "===================="
echo ""

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 1. 检查敏感信息泄露
echo "1. 检查敏感信息泄露..."
echo "----------------------------"
SENSITIVE_LOGS=$(grep -rn "password\|token\|secret" internal/ 2>/dev/null | grep -i "log\|print\|fmt" | grep -v "test")
if [ -n "$SENSITIVE_LOGS" ]; then
    echo -e "${RED}⚠️  发现可能的敏感信息泄露:${NC}"
    echo "$SENSITIVE_LOGS" | head -10
else
    echo -e "${GREEN}✅ 未发现敏感信息泄露${NC}"
fi
echo ""

# 2. 检查 SQL 注入风险
echo "2. 检查 SQL 注入风险..."
echo "----------------------------"
SQL_INJECTION=$(grep -rn "fmt.Sprintf.*SELECT\|fmt.Sprintf.*INSERT\|fmt.Sprintf.*UPDATE\|fmt.Sprintf.*DELETE" internal/ 2>/dev/null)
if [ -n "$SQL_INJECTION" ]; then
    echo -e "${RED}⚠️  发现潜在的 SQL 注入风险:${NC}"
    echo "$SQL_INJECTION" | head -10
else
    echo -e "${GREEN}✅ 未发现 SQL 注入风险${NC}"
fi
echo ""

# 3. 检查忽略的错误
echo "3. 检查忽略的错误..."
echo "----------------------------"
IGNORED_ERRORS=$(grep -rn "_ = \|_ =" internal/ 2>/dev/null | grep -v "test" | head -20)
if [ -n "$IGNORED_ERRORS" ]; then
    echo -e "${YELLOW}⚠️  发现忽略的错误 (${YELLOW}$(echo "$IGNORED_ERRORS" | wc -l)${NC} 处):"
    echo "$IGNORED_ERRORS"
else
    echo -e "${GREEN}✅ 未发现忽略的错误${NC}"
fi
echo ""

# 4. 检查硬编码的敏感信息
echo "4. 检查硬编码的敏感信息..."
echo "----------------------------"
HARDCODED_SECRETS=$(grep -rn '"password.*=.*"' internal/ 2>/dev/null | grep -v "test")
if [ -n "$HARDCODED_SECRETS" ]; then
    echo -e "${RED}⚠️  发现硬编码的敏感信息:${NC}"
    echo "$HARDCODED_SECRETS"
else
    echo -e "${GREEN}✅ 未发现硬编码的敏感信息${NC}"
fi
echo ""

# 5. 检查调试输出
echo "5. 检查调试输出..."
echo "----------------------------"
DEBUG_OUTPUT=$(grep -rn "fmt.Printf\|fmt.Println\|log.Printf.*%v" internal/ 2>/dev/null | grep -v "test")
if [ -n "$DEBUG_OUTPUT" ]; then
    echo -e "${YELLOW}⚠️  发现调试输出 (${YELLOW}$(echo "$DEBUG_OUTPUT" | wc -l)${NC} 处):"
    echo "$DEBUG_OUTPUT" | head -10
else
    echo -e "${GREEN}✅ 未发现调试输出${NC}"
fi
echo ""

# 6. 检查资源释放
echo "6. 检查资源释放..."
echo "----------------------------"
DEFER_CLOSE=$(grep -rn "defer.*Close\|defer.*Close()" internal/ 2>/dev/null | wc -l)
echo -e "${GREEN}✅ 发现 $DEFER_CLOSE 处资源释放${NC}"
echo ""

# 7. 检查并发安全
echo "7. 检查并发安全..."
echo "----------------------------"
MUTEX_COUNT=$(grep -rn "sync.Mutex\|sync.RWMutex" internal/ 2>/dev/null | wc -l)
GOROUTINE_COUNT=$(grep -rn "go func\|goroutine" internal/ 2>/dev/null | wc -l)
echo -e "${GREEN}✅ 发现 $MUTEX_COUNT 处锁使用${NC}"
echo -e "${GREEN}✅ 发现 $GOROUTINE_COUNT 处 goroutine 使用${NC}"
echo ""

# 8. 检查函数长度
echo "8. 检查超长函数..."
echo "----------------------------"
LONG_FUNCTIONS=$(awk '/^func / {start=NR; name=$0} /^}$/ && start {len=NR-start; if (len>200) print FILENAME":"start":"name" (len lines)"; start=0}' $(find internal/ -name "*.go") | head -10)
if [ -n "$LONG_FUNCTIONS" ]; then
    echo -e "${YELLOW}⚠️  发现超长函数 (>200 行):${NC}"
    echo "$LONG_FUNCTIONS"
else
    echo -e "${GREEN}✅ 未发现超长函数${NC}"
fi
echo ""

# 9. 检查 TODO 注释
echo "9. 检查未完成的 TODO..."
echo "----------------------------"
TODO_COUNT=$(grep -rn "TODO\|FIXME\|XXX" internal/ 2>/dev/null | grep -v "test" | wc -l)
if [ $TODO_COUNT -gt 0 ]; then
    echo -e "${YELLOW}⚠️  发现 $TODO_COUNT 处 TODO/FIXME${NC}"
    grep -rn "TODO\|FIXME\|XXX" internal/ 2>/dev/null | grep -v "test" | head -10
else
    echo -e "${GREEN}✅ 未发现未完成的 TODO${NC}"
fi
echo ""

# 10. 统计信息
echo "10. 代码统计..."
echo "----------------------------"
TOTAL_FILES=$(find internal/ -name "*.go" | wc -l)
TOTAL_LINES=$(find internal/ -name "*.go" -exec wc -l {} + | tail -1 | awk '{print $1}')
echo -e "总文件数: $TOTAL_FILES"
echo -e "总代码行: $TOTAL_LINES"
echo ""

echo "===================="
echo "✅ 安全检查完成"
echo ""
