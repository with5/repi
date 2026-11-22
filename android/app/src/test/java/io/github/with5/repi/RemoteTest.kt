package io.github.with5.repi

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RemoteTest {
    @Test
    fun normalizeBase() {
        assertEquals("http://192.168.1.5:7849", RemoteClient.normalizeBase(" 192.168.1.5 "))
        assertEquals("http://192.168.1.5:9000", RemoteClient.normalizeBase("192.168.1.5:9000/"))
        assertEquals("https://repi.example:7849", RemoteClient.normalizeBase("https://repi.example"))
    }

    @Test
    fun chinese() {
        assertTrue(isChinese("今天天气很好。"))
        assertTrue(isChinese("我在用 Kokoro 合成。"))
        assertFalse(isChinese("Say hello to the world: 你好"))
        assertFalse(isChinese("Hello world."))
    }

    @Test
    fun syncStatus() {
        fun mat(id: String, src: String?) = Material(
            id, java.io.File(id),
            parseManifest(
                """{"format":1,"title":"t",${src ?: ""}"sentences":[{"text":"a","audio":"audio/0001.opus","durationMs":1}]}""",
            ),
            0,
        )
        val phone = listOf(
            mat("a", """"source":{"server":"pc","id":1,"hash":"h1"},"""),
            mat("b", """"source":{"server":"pc","id":2,"hash":"old"},"""),
            mat("c", """"source":{"server":"other","id":3,"hash":"h3"},"""),
            mat("d", null),
        )
        assertEquals(SyncStatus.Synced, syncStatus(phone, "pc", DesktopMaterial(1, "x", hash = "h1")))
        assertEquals(SyncStatus.Changed, syncStatus(phone, "pc", DesktopMaterial(2, "x", hash = "h2")))
        assertEquals(SyncStatus.Missing, syncStatus(phone, "pc", DesktopMaterial(3, "x", hash = "h3")))
        assertEquals("b", phone.copyOf("pc", DesktopMaterial(2, "x"))?.id)
    }
}
