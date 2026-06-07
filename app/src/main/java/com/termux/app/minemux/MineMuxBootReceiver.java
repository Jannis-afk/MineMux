package com.termux.app.minemux;

import android.annotation.SuppressLint;
import android.app.job.JobInfo;
import android.app.job.JobScheduler;
import android.content.BroadcastReceiver;
import android.content.ComponentName;
import android.content.Context;
import android.content.Intent;
import android.os.PersistableBundle;
import android.util.Log;

import com.termux.shared.termux.TermuxConstants;

import java.io.File;
import java.util.Arrays;

public class MineMuxBootReceiver extends BroadcastReceiver {

    private static final String TAG = "MineMuxBoot";
    private static final int JOB_ID_BASE = 21000;
    private static int jobId = JOB_ID_BASE;

    @Override
    public void onReceive(Context context, Intent intent) {
        if (!Intent.ACTION_BOOT_COMPLETED.equals(intent.getAction())) return;

        MineMuxRuntime.ensureInstalled(context);

        @SuppressLint("SdCardPath")
        final File bootScriptDir = new File(TermuxConstants.TERMUX_BOOT_SCRIPTS_DIR_PATH);
        File[] files = bootScriptDir.listFiles();
        if (files == null) files = new File[0];
        Arrays.sort(files, (f1, f2) -> f1.getName().compareTo(f2.getName()));

        for (File file : files) {
            if (!file.isFile()) continue;
            if (!file.canRead()) //noinspection ResultOfMethodCallIgnored
                file.setReadable(true);
            if (!file.canExecute()) //noinspection ResultOfMethodCallIgnored
                file.setExecutable(true);

            PersistableBundle extras = new PersistableBundle();
            extras.putString(MineMuxBootJobService.SCRIPT_FILE_PATH, file.getAbsolutePath());

            ComponentName serviceComponent = new ComponentName(context, MineMuxBootJobService.class);
            JobInfo job = new JobInfo.Builder(jobId++, serviceComponent)
                .setExtras(extras)
                .setOverrideDeadline(3000)
                .build();
            JobScheduler scheduler = (JobScheduler) context.getSystemService(Context.JOB_SCHEDULER_SERVICE);
            if (scheduler != null) scheduler.schedule(job);
            Log.i(TAG, "Scheduled boot script " + file.getName());
        }
    }
}
