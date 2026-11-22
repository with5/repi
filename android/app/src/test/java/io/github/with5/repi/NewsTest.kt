package io.github.with5.repi

import org.junit.Assert.assertEquals
import org.junit.Test
import java.time.Instant
import java.time.ZoneId

class NewsTest {
    private val now = Instant.parse("2026-11-22T12:00:00Z")
    private val zone = ZoneId.of("Asia/Shanghai")

    @Test
    fun ago() {
        assertEquals("今天", ago("2026-11-22T01:00:00Z", now, zone))
        assertEquals("昨天", ago("2026-11-21T10:00:00Z", now, zone))
        assertEquals("3 天前", ago("2026-11-19T10:00:00Z", now, zone))
        assertEquals("10 月 28 日", ago("2026-10-28T00:00:00Z", now, zone))
        assertEquals("2025 年 3 月 1 日", ago("2025-03-01T00:00:00Z", now, zone))
        assertEquals("", ago("", now, zone))
        assertEquals("", ago("0001-01-01T00:00:00Z", now, zone))
    }

    @Test
    fun parseNews() {
        val r = json.decodeFromString<NewsResult>(
            """{"items":[{"source":"openai","title":"T","url":"https://o/x","published":"2026-11-20T10:00:00Z"},
               {"source":"huggingface","title":"H","url":"https://h/y"}],"errors":{"anthropic":"HTTP 503"}}""",
        )
        assertEquals(2, r.items.size)
        assertEquals("", r.items[1].summary)
        assertEquals("HTTP 503", r.errors["anthropic"])
    }
}
