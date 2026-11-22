package io.github.with5.repi

import android.app.PendingIntent
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import androidx.media3.common.AudioAttributes
import androidx.media3.common.C
import androidx.media3.common.MediaItem
import androidx.media3.common.MediaMetadata
import androidx.media3.common.Player
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.session.MediaSession
import androidx.media3.session.MediaSessionService
import androidx.media3.session.SessionCommand
import androidx.media3.session.SessionResult
import com.google.common.util.concurrent.Futures
import com.google.common.util.concurrent.ListenableFuture

/**
 * Plays a material's sentences, one media item each, in the background. The
 * session gives the lock screen, the notification and the headset buttons;
 * the app's screen is a MediaController like them.
 *
 * Looping the whole text is REPEAT_MODE_ALL. Repeating each sentence N times
 * is REPEAT_MODE_ONE until the sentence has been heard N times, then
 * REPEAT_MODE_ALL for its last time, so it moves on.
 */
class PlaybackService : MediaSessionService() {
    private lateinit var player: ExoPlayer
    private lateinit var session: MediaSession
    private lateinit var library: Library

    private var material: Material? = null
    private var mode = Mode.LoopAll
    private var repeat = 1
    private var rep = 0 // times the current sentence has been heard, less one

    override fun onCreate() {
        super.onCreate()
        library = Library.get(this)
        player = ExoPlayer.Builder(this)
            .setAudioAttributes(
                AudioAttributes.Builder()
                    .setUsage(C.USAGE_MEDIA)
                    .setContentType(C.AUDIO_CONTENT_TYPE_SPEECH)
                    .build(),
                /* handleAudioFocus = */ true,
            )
            .setHandleAudioBecomingNoisy(true) // pause when the headphones come out
            .setWakeMode(C.WAKE_MODE_LOCAL)
            // "previous" on a headset replays the sentence unless it just began
            .setMaxSeekToPreviousPositionMs(1500)
            .build()
        player.setPlaybackSpeed(Settings(this).speed)
        player.addListener(listener)

        val open = PendingIntent.getActivity(
            this, 0, Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        session = MediaSession.Builder(this, player)
            .setCallback(callback)
            .setSessionActivity(open)
            .build()
    }

    override fun onGetSession(controllerInfo: MediaSession.ControllerInfo) = session

    override fun onTaskRemoved(rootIntent: Intent?) {
        if (!player.playWhenReady || player.mediaItemCount == 0) stopSelf()
    }

    override fun onDestroy() {
        saveIndex()
        session.release()
        player.release()
        super.onDestroy()
    }

    private val listener = object : Player.Listener {
        override fun onMediaItemTransition(item: MediaItem?, reason: Int) {
            if (reason == Player.MEDIA_ITEM_TRANSITION_REASON_REPEAT) {
                rep++
            } else {
                rep = 0
                saveIndex()
            }
            applyRepeatMode()
            publishState()
        }

        override fun onTimelineChanged(timeline: androidx.media3.common.Timeline, reason: Int) {
            if (timeline.isEmpty && material != null) { // cleared, as when it is deleted
                material = null
                publishState()
            }
        }
    }

    private fun applyRepeatMode() {
        player.repeatMode = if (mode == Mode.RepeatEach && rep + 1 < repeat) {
            Player.REPEAT_MODE_ONE
        } else {
            Player.REPEAT_MODE_ALL
        }
    }

    private fun saveIndex() {
        val m = material ?: return
        if (player.mediaItemCount > 0) library.saveIndex(m.id, player.currentMediaItemIndex)
    }

    /** Tells the controllers the mode and the repetition being played. */
    private fun publishState() {
        session.setSessionExtras(Bundle().apply {
            putString(KEY_MATERIAL, material?.id)
            putInt(KEY_MODE, mode.ordinal)
            putInt(KEY_REPEAT, repeat)
            putInt(KEY_REP, rep)
        })
    }

    private fun items(m: Material): List<MediaItem> = m.manifest.sentences.mapIndexed { i, s ->
        MediaItem.Builder()
            .setMediaId("${m.id}/$i")
            .setUri(Uri.fromFile(m.audio(i)))
            .setMediaMetadata(
                MediaMetadata.Builder()
                    .setTitle(s.text) // the lock screen shows the sentence
                    .setArtist(m.manifest.title)
                    .setAlbumTitle(m.manifest.title)
                    .setTrackNumber(i + 1)
                    .setTotalTrackCount(m.manifest.sentences.size)
                    .setIsPlayable(true)
                    .setIsBrowsable(false)
                    .setMediaType(MediaMetadata.MEDIA_TYPE_MUSIC)
                    .build(),
            )
            .build()
    }

    /** Loads material [id] at sentence [index], or its saved one when -1; plays it if [play]. */
    private fun open(id: String, play: Boolean, index: Int): Boolean {
        val m = library.get(id) ?: return false
        if (material?.id != id) {
            saveIndex()
            val p = library.progress(id)
            material = m
            mode = p.mode
            repeat = p.repeat
            rep = 0
            val start = if (index >= 0) index else p.index
            player.setMediaItems(items(m), start.coerceIn(0, m.manifest.sentences.size - 1), 0)
            player.prepare()
            applyRepeatMode()
            library.markOpened(id)
            publishState()
        } else if (index >= 0) {
            player.seekTo(index, 0)
        }
        if (play) player.play()
        return true
    }

    private val callback = object : MediaSession.Callback {
        override fun onConnect(
            session: MediaSession,
            controller: MediaSession.ControllerInfo,
        ): MediaSession.ConnectionResult {
            val result = MediaSession.ConnectionResult.AcceptedResultBuilder(session, controller)
            // Only the app's own screen may open materials and change the mode;
            // the lock screen, headsets and other apps get the default commands.
            if (controller.packageName == packageName) {
                result.setAvailableSessionCommands(
                    MediaSession.ConnectionResult.DEFAULT_SESSION_COMMANDS.buildUpon()
                        .add(SessionCommand(CMD_OPEN, Bundle.EMPTY))
                        .add(SessionCommand(CMD_MODE, Bundle.EMPTY))
                        .build(),
                )
            }
            return result.build()
        }

        override fun onPostConnect(session: MediaSession, controller: MediaSession.ControllerInfo) {
            publishState()
        }

        override fun onCustomCommand(
            session: MediaSession,
            controller: MediaSession.ControllerInfo,
            customCommand: SessionCommand,
            args: Bundle,
        ): ListenableFuture<SessionResult> {
            when (customCommand.customAction) {
                CMD_OPEN -> {
                    val id = args.getString(KEY_MATERIAL)
                    if (id == null || !open(id, args.getBoolean(KEY_PLAY, true), args.getInt(KEY_INDEX, -1))) {
                        return Futures.immediateFuture(SessionResult(SessionResult.RESULT_ERROR_BAD_VALUE))
                    }
                }
                CMD_MODE -> {
                    mode = Mode.entries.getOrElse(args.getInt(KEY_MODE)) { Mode.LoopAll }
                    repeat = args.getInt(KEY_REPEAT, repeat).coerceIn(1, Library.MAX_REPEAT)
                    rep = 0
                    applyRepeatMode()
                    material?.let {
                        library.saveProgress(it.id, Progress(player.currentMediaItemIndex, mode, repeat))
                    }
                    publishState()
                }
                else -> return super.onCustomCommand(session, controller, customCommand, args)
            }
            return Futures.immediateFuture(SessionResult(SessionResult.RESULT_SUCCESS))
        }

        // A headset's play button, or the system's "resume" card, after the
        // service was gone: carry on with the material heard last.
        override fun onPlaybackResumption(
            mediaSession: MediaSession,
            controller: MediaSession.ControllerInfo,
            isForPlayback: Boolean,
        ): ListenableFuture<MediaSession.MediaItemsWithStartPosition> {
            val m = library.last?.let { library.get(it) } ?: library.materials.value.firstOrNull()
                ?: return Futures.immediateFailedFuture(UnsupportedOperationException("nothing to resume"))
            val p = library.progress(m.id)
            material = m
            mode = p.mode
            repeat = p.repeat
            rep = 0
            applyRepeatMode()
            publishState()
            return Futures.immediateFuture(
                MediaSession.MediaItemsWithStartPosition(
                    items(m), p.index.coerceIn(0, m.manifest.sentences.size - 1), 0,
                ),
            )
        }
    }

    companion object {
        const val CMD_OPEN = "repi.open"
        const val CMD_MODE = "repi.mode"
        const val KEY_MATERIAL = "material"
        const val KEY_PLAY = "play"
        const val KEY_INDEX = "index"
        const val KEY_MODE = "mode"
        const val KEY_REPEAT = "repeat"
        const val KEY_REP = "rep"
    }
}
