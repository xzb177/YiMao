#!/usr/bin/env python3
"""Test Rich Message functionality"""
import json
import httpx
import asyncio

async def test_rich_message():
    bot_token = "8520918593:***"
    chat_id = 5779291957
    
    # Test 1: Media Info Card
    print("🎴 Test 1: Media Info Card")
    media_info = {
        "blocks": [
            {
                "type": "heading",
                "text": {"type": "text", "text": "📺 《流浪地球3》"},
                "size": 2
            },
            {
                "type": "table",
                "cells": [
                    [
                        {"text": "项目", "is_header": True, "align": "center", "valign": "middle"},
                        {"text": "详情", "is_header": True, "align": "center", "valign": "middle"}
                    ],
                    [
                        {"text": "评分", "align": "left", "valign": "middle"},
                        {"text": "⭐ 8.5", "align": "left", "valign": "middle"}
                    ],
                    [
                        {"text": "年份", "align": "left", "valign": "middle"},
                        {"text": "2026", "align": "left", "valign": "middle"}
                    ],
                    [
                        {"text": "类型", "align": "left", "valign": "middle"},
                        {"text": "科幻/冒险", "align": "left", "valign": "middle"}
                    ]
                ],
                "is_bordered": True,
                "is_striped": True
            },
            {
                "type": "details",
                "text": {"type": "text", "text": "📝 剧情简介（点击展开）"},
                "items": [
                    {
                        "type": "paragraph",
                        "text": {"type": "text", "text": "太阳即将毁灭，人类启动流浪地球计划，试图带着地球逃离太阳系，寻找新的家园。"}
                    }
                ]
            }
        ]
    }
    
    async with httpx.AsyncClient() as client:
        url = f"https://api.telegram.org/bot{bot_token}/sendRichMessage"
        data = {
            "chat_id": chat_id,
            "rich_message": media_info
        }
        
        response = await client.post(url, json=data, timeout=10)
        result = response.json()
        
        if result.get("ok"):
            print(f"✅ Media info card sent! Message ID: {result['result']['message_id']}")
        else:
            print(f"❌ Failed: {result}")
            return
    
    # Test 2: Subscription Dashboard
    print("\n📊 Test 2: Subscription Dashboard")
    subscription_dashboard = {
        "blocks": [
            {
                "type": "heading",
                "text": {"type": "text", "text": "📋 我的订阅状态"},
                "size": 2
            },
            {
                "type": "table",
                "cells": [
                    [
                        {"text": "影视", "is_header": True, "align": "center", "valign": "middle"},
                        {"text": "状态", "is_header": True, "align": "center", "valign": "middle"},
                        {"text": "进度", "is_header": True, "align": "center", "valign": "middle"}
                    ],
                    [
                        {"text": "流浪地球3", "align": "left", "valign": "middle"},
                        {"text": "⬇️ 下载中", "align": "left", "valign": "middle"},
                        {"text": "███████░░░ 70%", "align": "left", "valign": "middle"}
                    ],
                    [
                        {"text": "三体 S2", "align": "left", "valign": "middle"},
                        {"text": "✅ 已入库", "align": "left", "valign": "middle"},
                        {"text": "██████████ 100%", "align": "left", "valign": "middle"}
                    ],
                    [
                        {"text": "庆余年3", "align": "left", "valign": "middle"},
                        {"text": "🔍 搜索中", "align": "left", "valign": "middle"},
                        {"text": "░░░░░░░░░░ 0%", "align": "left", "valign": "middle"}
                    ]
                ],
                "is_bordered": True,
                "is_striped": True
            },
            {
                "type": "paragraph",
                "text": {"type": "text", "text": "📊 今日新增：2 部 | 本周下载：5 部"}
            }
        ]
    }
    
    async with httpx.AsyncClient() as client:
        url = f"https://api.telegram.org/bot{bot_token}/sendRichMessage"
        data = {
            "chat_id": chat_id,
            "rich_message": subscription_dashboard
        }
        
        response = await client.post(url, json=data, timeout=10)
        result = response.json()
        
        if result.get("ok"):
            print(f"✅ Subscription dashboard sent! Message ID: {result['result']['message_id']}")
        else:
            print(f"❌ Failed: {result}")

if __name__ == "__main__":
    asyncio.run(test_rich_message())
