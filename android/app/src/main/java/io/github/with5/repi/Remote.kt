package io.github.with5.repi

import android.app.Application
import android.content.Context
import android.net.Uri
import androidx.core.content.edit
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import androidx.media3.common.MediaItem
import androidx.media3.exoplayer.ExoPlayer
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable
import java.io.File
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL

/** A voice of the computer, as `repi serve` lists it. */
@Serializable
data class RemoteVoice(val name: String, val label: String, val detail: String = "", val lang: String = "en")

/** A pack the computer is making. */
@Serializable
data class RemoteJob(
    val id: String,
    val title: String = "",
    val state: String,
    val done: Int = 0,
    val total: Int = 0,
    val error: String? = null,
)

@Serializable
private data class JobRequest(
    val title: String,
    val text: String,
    val voice: String,
    val speed: Double,
    val translate: Boolean,
)

/** A material of the computer's library, as `repi serve` lists it. */
@Serializable
data class DesktopMaterial(
    val id: Int,
    val title: String,
    val lang: String = "en",
    val sentences: Int = 0,
    val hash: String = "",
)

/** Where a material of the computer stands on the phone. */
enum class SyncStatus { Missing, Synced, Changed }

@Serializable
private data class MaterialJobRequest(val translate: Boolean)

@Serializable
private data class ServerInfo(val id: String = "")

@Serializable
private data class ErrorReply(val error: String = "")

class RemoteException(message: String) : IOException(message)


/** Talks to `repi serve` on the computer at [base], e.g. "http://192.168.1.5:7849". */
class RemoteClient(base: String, private val token: String) {
    private val base = normalizeBase(base)

    /** This computer's ID, which tells its materials from another's. */
    suspend fun id(): String = json.decodeFromString<ServerInfo>(request("GET", "/api/v1/info").decodeToString()).id

    suspend fun materials(): List<DesktopMaterial> =
        json.decodeFromString(request("GET", "/api/v1/materials").decodeToString())

    /** Starts a pack of material [id], in the voice and speed the computer reads it with. */
    suspend fun startMaterial(id: Int, translate: Boolean): RemoteJob = json.decodeFromString(
        request(
            "POST", "/api/v1/materials/$id/jobs",
            json.encodeToString(MaterialJobRequest.serializer(), MaterialJobRequest(translate)).encodeToByteArray(),
        ).decodeToString(),
    )

    suspend fun newsSources(): List<NewsSource> =
        json.decodeFromString(request("GET", "/api/v1/news/sources").decodeToString())

    /** The newest posts of [sources] (all when empty); [refresh] has the computer fetch them again. */
    suspend fun news(sources: List<String>, refresh: Boolean): NewsResult {
        val q = buildList {
            if (sources.isNotEmpty()) add("source=" + enc(sources.joinToString(",")))
            if (refresh) add("refresh=1")
        }.joinToString("&")
        return json.decodeFromString(request("GET", "/api/v1/news" + if (q.isEmpty()) "" else "?$q", timeoutMs = 60_000).decodeToString())
    }

    /** The text of the post at [url], extracted by the computer. */
    suspend fun article(url: String): ArticleText =
        json.decodeFromString(request("GET", "/api/v1/article?url=" + enc(url), timeoutMs = 60_000).decodeToString())

    private fun enc(s: String) = java.net.URLEncoder.encode(s, "UTF-8")

    suspend fun voices(): List<RemoteVoice> =
        json.decodeFromString(request("GET", "/api/v1/voices").decodeToString())

    /** The audio of the first sentence of [text], for an audition. */
    suspend fun speak(text: String, voice: String, speed: Double): ByteArray =
        request("POST", "/api/v1/speak", body(JobRequest("", text, voice, speed, false)), timeoutMs = 120_000)

    suspend fun start(title: String, text: String, voice: String, speed: Double, translate: Boolean): RemoteJob =
        json.decodeFromString(request("POST", "/api/v1/jobs", body(JobRequest(title, text, voice, speed, translate))).decodeToString())

    suspend fun job(id: String): RemoteJob = json.decodeFromString(request("GET", "/api/v1/jobs/$id").decodeToString())

    /** Downloads the finished pack of job [id] into [out]. */
    suspend fun download(id: String, out: File) {
        request("GET", "/api/v1/jobs/$id/pack", into = out, timeoutMs = 300_000)
    }

    suspend fun delete(id: String) {
        runCatching { request("DELETE", "/api/v1/jobs/$id") }
    }

    private fun body(r: JobRequest) = json.encodeToString(JobRequest.serializer(), r).encodeToByteArray()

    private suspend fun request(
        method: String,
        path: String,
        body: ByteArray? = null,
        into: File? = null,
        timeoutMs: Int = 15_000,
    ): ByteArray = withContext(Dispatchers.IO) {
        val c = try {
            URL(base + path).openConnection() as HttpURLConnection
        } catch (e: Exception) {
            throw RemoteException("地址不对：$base")
        }
        try {
            c.requestMethod = method
            c.connectTimeout = 5_000
            c.readTimeout = timeoutMs
            c.setRequestProperty("Authorization", "Bearer $token")
            if (body != null) {
                c.doOutput = true
                c.setRequestProperty("Content-Type", "application/json")
                c.setFixedLengthStreamingMode(body.size)
                c.outputStream.use { it.write(body) }
            }
            val code = try {
                c.responseCode
            } catch (e: IOException) {
                throw RemoteException("连不上电脑（${e.message ?: e.javaClass.simpleName}）。电脑上运行了 repi serve 吗？手机和电脑在同一个网络吗？")
            }
            if (code !in 200..299) {
                val msg = c.errorStream?.use { it.readBytes().decodeToString() }
                    ?.let { runCatching { json.decodeFromString(ErrorReply.serializer(), it).error }.getOrNull() }
                throw RemoteException(msg?.takeIf { it.isNotBlank() } ?: "电脑返回 $code")
            }
            if (into != null) {
                c.inputStream.use { src -> into.outputStream().use { src.copyTo(it) } }
                ByteArray(0)
            } else {
                c.inputStream.use { it.readBytes() }
            }
        } finally {
            c.disconnect()
        }
    }

    companion object {
        /** Adds http:// and the default port to what the user typed, as needed. */
        fun normalizeBase(s: String): String {
            var b = s.trim().trimEnd('/')
            if (!b.startsWith("http://") && !b.startsWith("https://")) b = "http://$b"
            val host = b.substringAfter("://")
            if (!host.contains(':')) b += ":$DEFAULT_PORT"
            return b
        }

        const val DEFAULT_PORT = 7849
    }
}

/** Where the computer is, saved between runs. */
class RemoteSettings(context: Context) {
    private val prefs = context.applicationContext.getSharedPreferences("remote", Context.MODE_PRIVATE)
    var url: String
        get() = prefs.getString("url", "") ?: ""
        set(v) = prefs.edit { putString("url", v) }
    var token: String
        get() = prefs.getString("token", "") ?: ""
        set(v) = prefs.edit { putString("token", v) }
    var voice: String
        get() = prefs.getString("voice", "") ?: ""
        set(v) = prefs.edit { putString("voice", v) }
    var zhVoice: String
        get() = prefs.getString("zhVoice", "") ?: ""
        set(v) = prefs.edit { putString("zhVoice", v) }
    var speed: Float
        get() = prefs.getFloat("speed", 1f)
        set(v) = prefs.edit { putFloat("speed", v) }
    var translate: Boolean
        get() = prefs.getBoolean("translate", true)
        set(v) = prefs.edit { putBoolean("translate", v) }
}

data class RemoteState(
    val connecting: Boolean = false,
    val voices: List<RemoteVoice> = emptyList(),
    val connectError: String? = null,
    val voice: String = "",
    val zhVoice: String = "",
    val speed: Float = 1f,
    val translate: Boolean = true,
    /** The voice being auditioned, and whether it is still being made. */
    val auditioning: String? = null,
    val auditionLoading: Boolean = false,
    val job: RemoteJob? = null,
    val downloading: Boolean = false,
    val error: String? = null,
    /** The computer's ID and its materials. */
    val serverId: String = "",
    val desktop: List<DesktopMaterial> = emptyList(),
    /** While syncing: the material being made, and which of how many it is. */
    val syncing: DesktopMaterial? = null,
    val syncIndex: Int = 0,
    val syncCount: Int = 0,
) {
    val busy get() = job != null || downloading || syncing != null
}

/** The phone's copy of desktop material [m] from server [serverId], if it has one. */
fun List<Material>.copyOf(serverId: String, m: DesktopMaterial): Material? =
    find { it.manifest.source?.let { s -> s.server == serverId && s.id == m.id } == true }

fun syncStatus(phone: List<Material>, serverId: String, m: DesktopMaterial): SyncStatus {
    val c = phone.copyOf(serverId, m) ?: return SyncStatus.Missing
    return if (c.manifest.source?.hash == m.hash) SyncStatus.Synced else SyncStatus.Changed
}

/**
 * True when [text] is Chinese, as the desktop's text.IsChinese decides it:
 * it has Chinese characters, at least as many as Latin words.
 */
fun isChinese(text: String): Boolean {
    var han = 0
    var words = 0
    var inWord = false
    var i = 0
    while (i < text.length) {
        val cp = text.codePointAt(i)
        val latin = cp < 0x250 && Character.isLetter(cp)
        if (latin && !inWord) words++
        inWord = latin
        if (Character.UnicodeScript.of(cp) == Character.UnicodeScript.HAN) han++
        i += Character.charCount(cp)
    }
    return han > 0 && han >= words
}

/** The "电脑合成" screen: has the computer read a text and imports the result. */
class RemoteModel(app: Application) : AndroidViewModel(app) {
    private val settings = RemoteSettings(app)
    val library = Library.get(app)

    private val _state = MutableStateFlow(
        RemoteState(voice = settings.voice, zhVoice = settings.zhVoice, speed = settings.speed, translate = settings.translate),
    )
    val state: StateFlow<RemoteState> = _state.asStateFlow()

    val url get() = settings.url
    val token get() = settings.token

    private var client: RemoteClient? = null
    private var player: ExoPlayer? = null
    private var audition: Job? = null
    private var work: Job? = null

    init {
        if (settings.url.isNotBlank() && settings.token.isNotBlank()) connect(settings.url, settings.token)
    }

    fun connect(url: String, token: String) {
        settings.url = url.trim()
        settings.token = token.trim()
        val c = RemoteClient(settings.url, settings.token)
        client = c
        _state.update { it.copy(connecting = true, connectError = null) }
        viewModelScope.launch {
            try {
                val v = c.voices()
                val id = c.id()
                val mats = c.materials()
                _state.update { s ->
                    s.copy(
                        connecting = false, voices = v, serverId = id, desktop = mats,
                        voice = pick(v, "en", s.voice), zhVoice = pick(v, "zh", s.zhVoice),
                    )
                }
            } catch (e: Exception) {
                _state.update { it.copy(connecting = false, voices = emptyList(), connectError = e.message ?: e.javaClass.simpleName) }
            }
        }
    }

    /** Asks the computer again for its voices and materials. */
    fun refresh() {
        if (settings.url.isNotBlank() && settings.token.isNotBlank() && !_state.value.connecting) {
            connect(settings.url, settings.token)
        }
    }

    private fun pick(voices: List<RemoteVoice>, lang: String, want: String): String =
        voices.find { it.name == want && it.lang == lang }?.name
            ?: voices.find { it.lang == lang && it.name.startsWith("kokoro:") }?.name
            ?: voices.find { it.lang == lang }?.name
            ?: want

    fun choose(v: RemoteVoice) {
        if (v.lang == "zh") {
            settings.zhVoice = v.name
            _state.update { it.copy(zhVoice = v.name) }
        } else {
            settings.voice = v.name
            _state.update { it.copy(voice = v.name) }
        }
    }

    fun setSpeed(v: Float) {
        settings.speed = v
        _state.update { it.copy(speed = v) }
    }

    fun setTranslate(v: Boolean) {
        settings.translate = v
        _state.update { it.copy(translate = v) }
    }

    /** Plays [sample] in voice [v]; asking again for the same voice stops it. */
    fun audition(v: RemoteVoice, sample: String) {
        val c = client ?: return
        val again = _state.value.auditioning == v.name
        stopAudition()
        if (again) return
        _state.update { it.copy(auditioning = v.name, auditionLoading = true, error = null) }
        audition = viewModelScope.launch {
            try {
                val audio = c.speak(sample, v.name, _state.value.speed.toDouble())
                val f = withContext(Dispatchers.IO) {
                    File(getApplication<Application>().cacheDir, "audition").also { it.writeBytes(audio) }
                }
                val p = player ?: ExoPlayer.Builder(getApplication()).build().also { player = it }
                p.setMediaItem(MediaItem.fromUri(Uri.fromFile(f)))
                p.prepare()
                p.play()
                _state.update { it.copy(auditionLoading = false) }
                while (p.playbackState != ExoPlayer.STATE_ENDED && p.playerError == null) delay(100)
                _state.update { it.copy(auditioning = null) }
            } catch (e: Exception) {
                if (e is kotlinx.coroutines.CancellationException) throw e
                _state.update { it.copy(auditioning = null, auditionLoading = false, error = "试听失败：" + (e.message ?: "")) }
            }
        }
    }

    fun stopAudition() {
        audition?.cancel()
        audition = null
        player?.stop()
        _state.update { it.copy(auditioning = null, auditionLoading = false) }
    }

    /** Has the computer make a pack of [text], then imports it; calls [done] with the new material. */
    fun make(title: String, text: String, done: (Material) -> Unit) {
        val c = client ?: return
        if (work?.isActive == true) return
        stopAudition()
        val s = _state.value
        val zh = isChinese(text)
        val voice = if (zh) s.zhVoice else s.voice
        _state.update { it.copy(error = null, job = null) }
        work = viewModelScope.launch {
            try {
                val m = fetch(c) { c.start(title, text, voice, s.speed.toDouble(), s.translate && !zh) }
                done(m)
            } catch (e: Exception) {
                if (e is kotlinx.coroutines.CancellationException) throw e
                _state.update { it.copy(error = e.message ?: e.javaClass.simpleName) }
            }
        }
    }

    /**
     * Brings the computer's materials [items] to the phone one after another.
     * A material the phone has an older copy of replaces it, keeping where
     * the listener was; [replaced] is told the old copy's ID, to remove it.
     */
    fun sync(items: List<DesktopMaterial>, replaced: (old: String) -> Unit) {
        val c = client ?: return
        if (work?.isActive == true || items.isEmpty()) return
        stopAudition()
        val s = _state.value
        _state.update { it.copy(error = null, job = null) }
        work = viewModelScope.launch {
            var ok = 0
            try {
                items.forEachIndexed { i, m ->
                    _state.update { it.copy(syncing = m, syncIndex = i, syncCount = items.size) }
                    val old = library.materials.value.copyOf(s.serverId, m)
                    val new = fetch(c) { c.startMaterial(m.id, s.translate && m.lang != "zh") }
                    if (old != null) {
                        library.moveProgress(old.id, new.id)
                        replaced(old.id)
                    }
                    ok++
                }
                _state.update { it.copy(error = if (items.size > 1) "已同步 $ok 篇" else "已同步：${items[0].title}") }
            } catch (e: Exception) {
                if (e is kotlinx.coroutines.CancellationException) throw e
                val msg = e.message ?: e.javaClass.simpleName
                _state.update { it.copy(error = if (ok > 0) "已同步 $ok 篇，之后失败：$msg" else "同步失败：$msg") }
            } finally {
                _state.update { it.copy(syncing = null, syncIndex = 0, syncCount = 0) }
            }
        }
    }

    /** Runs a job [start]s on the computer, then downloads and imports its pack. */
    private suspend fun fetch(c: RemoteClient, start: suspend () -> RemoteJob): Material {
        var id: String? = null
        try {
            var j = start()
            id = j.id
            _state.update { it.copy(job = j) }
            while (j.state == "running") {
                delay(700)
                j = c.job(j.id)
                _state.update { it.copy(job = j) }
            }
            if (j.state != "done") throw RemoteException(j.error ?: "电脑没能做完")
            _state.update { it.copy(downloading = true) }
            val f = File(getApplication<Application>().cacheDir, "remote-${j.id}.repi")
            try {
                c.download(j.id, f)
                return library.import(Uri.fromFile(f))
            } finally {
                f.delete()
            }
        } finally {
            _state.update { it.copy(job = null, downloading = false) }
            id?.let { withContext(NonCancellable) { c.delete(it) } }
        }
    }

    fun cancel() {
        work?.cancel()
        work = null
        _state.update { it.copy(job = null, downloading = false, syncing = null) }
    }

    fun errorShown() = _state.update { it.copy(error = null) }

    override fun onCleared() {
        player?.release()
    }
}
