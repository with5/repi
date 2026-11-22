package io.github.with5.repi

import android.app.Application
import android.content.ComponentName
import android.net.Uri
import android.os.Bundle
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import androidx.media3.common.Player
import androidx.media3.session.MediaController
import androidx.media3.session.SessionCommand
import androidx.media3.session.SessionToken
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.guava.await
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch

/** What the screens show of playback. */
data class PlayState(
    val materialId: String? = null,
    val index: Int = 0,
    val playing: Boolean = false,
    val positionMs: Long = 0,
    val mode: Mode = Mode.LoopAll,
    val repeat: Int = 3,
    val rep: Int = 0,
    val speed: Float = 1f,
)

/** The app's view of the playback service, through a MediaController. */
class PlayerModel(app: Application) : AndroidViewModel(app) {
    val library = Library.get(app)
    private val settings = Settings(app)

    private val _state = MutableStateFlow(PlayState(speed = settings.speed))
    val state: StateFlow<PlayState> = _state.asStateFlow()

    private val _showTranslation = MutableStateFlow(settings.showTranslation)
    val showTranslation: StateFlow<Boolean> = _showTranslation.asStateFlow()
    private val _hideText = MutableStateFlow(settings.hideText)
    val hideText: StateFlow<Boolean> = _hideText.asStateFlow()

    /** The material whose screen is open; null shows the library. */
    private val _openId = MutableStateFlow<String?>(null)
    val openId: StateFlow<String?> = _openId.asStateFlow()

    private val _message = MutableStateFlow<String?>(null)
    val message: StateFlow<String?> = _message.asStateFlow()

    private var controller: MediaController? = null

    init {
        viewModelScope.launch {
            val token = SessionToken(app, ComponentName(app, PlaybackService::class.java))
            val c = MediaController.Builder(app, token).buildAsync().await()
            controller = c
            c.addListener(object : Player.Listener {
                override fun onEvents(player: Player, events: Player.Events) = sync()
            })
            sync()
            // The word highlight follows the sound; and sessionExtras
            // changes do not come as player events.
            while (isActive) {
                delay(if (c.isPlaying) 50 else 250)
                sync()
            }
        }
    }

    private fun sync() {
        val c = controller ?: return
        val extras = c.sessionExtras
        _state.update {
            it.copy(
                materialId = if (extras.containsKey(PlaybackService.KEY_MATERIAL)) {
                    extras.getString(PlaybackService.KEY_MATERIAL)
                } else {
                    it.materialId
                },
                index = if (c.mediaItemCount > 0) c.currentMediaItemIndex else it.index,
                playing = c.playWhenReady,
                positionMs = c.currentPosition,
                mode = Mode.entries.getOrElse(extras.getInt(PlaybackService.KEY_MODE, it.mode.ordinal)) { Mode.LoopAll },
                repeat = extras.getInt(PlaybackService.KEY_REPEAT, it.repeat),
                rep = extras.getInt(PlaybackService.KEY_REP, 0),
                speed = c.playbackParameters.speed,
            )
        }
    }

    /** Opens the screen of material [id] and starts reading it. */
    fun open(id: String, play: Boolean = true, index: Int = -1) {
        _openId.value = id
        val c = controller
        if (c == null) {
            viewModelScope.launch { // not connected yet: try again shortly
                delay(200)
                open(id, play, index)
            }
            return
        }
        if (_state.value.materialId == id && c.mediaItemCount > 0) {
            if (index >= 0) c.seekTo(index, 0)
            if (play && !c.playWhenReady) c.play()
            return
        }
        val start = if (index >= 0) index else library.progress(id).index
        _state.update { it.copy(materialId = id, index = start, rep = 0) }
        c.sendCustomCommand(
            SessionCommand(PlaybackService.CMD_OPEN, Bundle.EMPTY),
            Bundle().apply {
                putString(PlaybackService.KEY_MATERIAL, id)
                putBoolean(PlaybackService.KEY_PLAY, play)
                putInt(PlaybackService.KEY_INDEX, index)
            },
        )
    }

    fun close() {
        _openId.value = null
    }

    fun togglePlay() {
        val c = controller ?: return
        if (c.playWhenReady) c.pause() else c.play()
    }

    fun pause() {
        controller?.pause()
    }

    fun next() = controller?.seekToNextMediaItem()
    fun previous() = controller?.seekToPreviousMediaItem()

    /** Jumps to sentence [i] and plays it. */
    fun seekTo(i: Int) {
        val c = controller ?: return
        c.seekTo(i, 0)
        c.play()
    }

    fun setMode(mode: Mode, repeat: Int) {
        _state.update { it.copy(mode = mode, repeat = repeat) }
        controller?.sendCustomCommand(
            SessionCommand(PlaybackService.CMD_MODE, Bundle.EMPTY),
            Bundle().apply {
                putInt(PlaybackService.KEY_MODE, mode.ordinal)
                putInt(PlaybackService.KEY_REPEAT, repeat)
            },
        )
    }

    fun setSpeed(speed: Float) {
        settings.speed = speed
        controller?.setPlaybackSpeed(speed)
        _state.update { it.copy(speed = speed) }
    }

    fun setShowTranslation(v: Boolean) {
        settings.showTranslation = v
        _showTranslation.value = v
    }

    fun setHideText(v: Boolean) {
        settings.hideText = v
        _hideText.value = v
    }

    /** Imports a pack and opens it, paused. */
    fun import(uri: Uri) {
        viewModelScope.launch {
            try {
                val m = library.import(uri)
                _message.value = "已导入：${m.manifest.title}"
                open(m.id, play = false)
            } catch (e: Exception) {
                _message.value = "导入失败：" + (e.message ?: e.javaClass.simpleName)
            }
        }
    }

    fun delete(id: String) {
        viewModelScope.launch {
            if (_state.value.materialId == id) {
                controller?.stop()
                controller?.clearMediaItems()
                _state.update { it.copy(materialId = null) }
            }
            library.delete(id)
        }
    }

    fun messageShown() {
        _message.value = null
    }

    override fun onCleared() {
        controller?.release()
    }
}
