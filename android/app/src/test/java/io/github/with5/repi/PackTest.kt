package io.github.with5.repi

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.File
import java.util.zip.ZipEntry
import java.util.zip.ZipOutputStream

class PackTest {
    @get:Rule val tmp = TemporaryFolder()

    private val manifest = """
        {"format":1,"title":"Test","lang":"en","voice":"kokoro:af_heart","speed":1,
         "created":"2026-11-21T10:00:00Z","gapMs":350,"future":"ignored",
         "sentences":[
           {"text":"Hello, world.","translation":"你好，世界。","audio":"audio/0001.opus","durationMs":1830,
            "tokens":[{"text":"Hello,","endMs":620},{"text":"world.","space":true,"endMs":1830}]},
           {"text":"Bye.","audio":"audio/0002.opus","durationMs":500,"tokens":[{"text":"Bye.","endMs":500}]}
         ]}
    """.trimIndent()

    private fun zip(vararg entries: Pair<String, ByteArray>): ByteArray {
        val out = ByteArrayOutputStream()
        ZipOutputStream(out).use { z ->
            for ((name, body) in entries) {
                z.putNextEntry(ZipEntry(name))
                z.write(body)
                z.closeEntry()
            }
        }
        return out.toByteArray()
    }

    private fun good() = zip(
        "manifest.json" to manifest.toByteArray(),
        "audio/0001.opus" to byteArrayOf(1, 2, 3),
        "audio/0002.opus" to byteArrayOf(4, 5),
        "../evil.txt" to byteArrayOf(9),
    )

    @Test fun importsManifestAndAudio() {
        val packs = tmp.newFolder("packs")
        val dir = importPack(ByteArrayInputStream(good()), packs)
        val man = parseManifest(File(dir, "manifest.json").readText())
        assertEquals("Test", man.title)
        assertEquals(2, man.sentences.size)
        assertEquals("你好，世界。", man.sentences[0].translation)
        assertEquals(null, man.sentences[1].translation)
        assertEquals(3L, File(dir, "audio/0001.opus").length())
        // Only the named files come out, and no temporary file stays.
        assertFalse(File(tmp.root, "evil.txt").exists())
        assertFalse(File(packs, "evil.txt").exists())
        assertEquals(listOf(dir.name), packs.list()!!.toList())
    }

    @Test fun refusesBrokenPacksAndCleansUp() {
        val packs = tmp.newFolder("packs")
        val cases = listOf(
            "not a zip".toByteArray(),
            zip("other.txt" to byteArrayOf(1)),
            zip("manifest.json" to manifest.toByteArray(), "audio/0001.opus" to byteArrayOf(1)), // 0002 missing
            zip("manifest.json" to manifest.replace("\"format\":1", "\"format\":2").toByteArray()),
            zip("manifest.json" to manifest.replace("audio/0001.opus", "../../x.opus").toByteArray()),
        )
        for ((i, b) in cases.withIndex()) {
            try {
                importPack(ByteArrayInputStream(b), packs)
                fail("case $i was imported")
            } catch (e: PackException) {
                assertTrue("case $i: ${e.message}", e.message!!.isNotEmpty())
            }
            assertEquals("case $i left files", 0, packs.list()!!.size)
        }
    }

    @Test fun tokenAt() {
        val s = parseManifest(manifest).sentences[0]
        assertEquals(0, s.tokenAt(0))
        assertEquals(0, s.tokenAt(619))
        assertEquals(1, s.tokenAt(620))
        assertEquals(1, s.tokenAt(5000)) // after the speech: the last word
        assertEquals(-1, s.copy(tokens = emptyList()).tokenAt(10))
    }

    /** A pack from the desktop exporter, when REPI_PACK names one. */
    @Test fun importsDesktopPack() {
        val path = System.getenv("REPI_PACK") ?: return
        val dir = importPack(File(path).inputStream(), tmp.newFolder("packs"))
        val man = parseManifest(File(dir, "manifest.json").readText())
        assertTrue(man.sentences.isNotEmpty())
        for (s in man.sentences) {
            assertTrue(File(dir, s.audio).length() > 0)
            assertTrue(s.tokens.isNotEmpty())
            assertTrue(s.tokens.zipWithNext().all { (a, b) -> a.endMs <= b.endMs })
        }
    }
}
