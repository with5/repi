package io.github.with5.repi

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import java.io.File
import java.io.IOException
import java.io.InputStream
import java.util.UUID
import java.util.zip.ZipFile

/** The newest manifest.json format this app reads; see internal/pack in the desktop app. */
const val PACK_FORMAT = 1

@Serializable
data class Manifest(
    val format: Int,
    val title: String,
    val lang: String = "en",
    val voice: String = "",
    val speed: Double = 1.0,
    val created: String = "",
    val gapMs: Long = 0,
    /** The desktop material this pack was synced from; null for an export. */
    val source: Origin? = null,
    val sentences: List<Sentence>,
)

/** Which computer ([server]) and material ([id]) a pack came from, and the [hash] of its text then. */
@Serializable
data class Origin(val server: String, val id: Int, val hash: String = "")

@Serializable
data class Sentence(
    val text: String,
    val translation: String? = null,
    val audio: String,
    val durationMs: Long,
    val tokens: List<Token> = emptyList(),
)

@Serializable
data class Token(
    val text: String,
    val space: Boolean = false,
    val endMs: Long,
)

/** The token being said at [posMs] into the sentence: the first that has not ended. */
fun Sentence.tokenAt(posMs: Long): Int {
    if (tokens.isEmpty()) return -1
    val i = tokens.indexOfFirst { posMs < it.endMs }
    return if (i < 0) tokens.size - 1 else i
}

class PackException(message: String) : IOException(message)

internal val json = Json { ignoreUnknownKeys = true }

/** The only names a pack may hold: no paths out of its folder. */
private val audioName = Regex("""audio/\d{4}\.(opus|wav|ogg|m4a|mp3)""")

private const val maxManifest = 16L shl 20
private const val maxAudio = 64L shl 20

/**
 * Unpacks a .repi file into a new folder under [packsDir] and returns it. Only
 * manifest.json and the audio it names are taken out; anything else in the zip
 * is ignored. On failure nothing is left behind.
 */
fun importPack(input: InputStream, packsDir: File): File {
    packsDir.mkdirs()
    val tmp = File.createTempFile("import", ".repi", packsDir)
    val dir = File(packsDir, UUID.randomUUID().toString())
    try {
        tmp.outputStream().use { input.copyTo(it) }
        val zip = try {
            ZipFile(tmp)
        } catch (e: IOException) {
            throw PackException("不是 Repi 导出的文件")
        }
        zip.use { z ->
            val entry = z.getEntry("manifest.json") ?: throw PackException("不是 Repi 导出的文件")
            if (entry.size > maxManifest) throw PackException("文件内容异常")
            val text = z.getInputStream(entry).use { it.readBytes().decodeToString() }
            val man = parseManifest(text)
            dir.mkdirs()
            for (s in man.sentences) {
                val e = z.getEntry(s.audio) ?: throw PackException("缺少音频 ${s.audio}")
                val out = File(dir, s.audio)
                out.parentFile!!.mkdirs()
                z.getInputStream(e).use { src ->
                    out.outputStream().use { dst -> copyLimited(src, dst, maxAudio) }
                }
            }
            File(dir, "manifest.json").writeText(text)
        }
        return dir
    } catch (e: Exception) {
        dir.deleteRecursively()
        throw e
    } finally {
        tmp.delete()
    }
}

/** Parses and checks manifest.json. */
fun parseManifest(text: String): Manifest {
    val man = try {
        json.decodeFromString<Manifest>(text)
    } catch (e: Exception) {
        throw PackException("文件内容异常")
    }
    if (man.format > PACK_FORMAT) throw PackException("这个文件来自更新版本的 Repi，请先升级手机应用")
    if (man.sentences.isEmpty()) throw PackException("文件里没有句子")
    for (s in man.sentences) {
        if (!audioName.matches(s.audio)) throw PackException("文件内容异常")
    }
    return man
}

private fun copyLimited(src: InputStream, dst: java.io.OutputStream, limit: Long) {
    val buf = ByteArray(64 shl 10)
    var total = 0L
    while (true) {
        val n = src.read(buf)
        if (n < 0) return
        total += n
        if (total > limit) throw PackException("音频文件过大")
        dst.write(buf, 0, n)
    }
}
