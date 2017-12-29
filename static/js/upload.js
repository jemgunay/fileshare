$(document).ready(function() {
    // init dropzone
    Dropzone.options.fileInput = {
        paramName: "file-input", // The name that will be used to transfer the file
        maxFilesize: 10, // MB
        init: function() {
            this.on("success", function(file, response) {
                $("#upload-results-panel").append(response);
                setAlertWindow("success", "'" + file.name + "' successfully uploaded!", "#error-window");

                initUploadForm();
            });
            this.on('error', function(file, errorMessage) {
                errorMessage = errorMessage.trim();
                var refinedError = "";

                //console.log("upload error (" + file.name + "): " + errorMessage);

                if (errorMessage === "already_uploaded") {
                    refinedError = "A copy of '" + file.name + "' has already been uploaded by another user, but not yet published."
                }
                else if (errorMessage === "already_published") {
                    refinedError = "A copy of '" + file.name + "' has already been published to memories by another user."
                }
                if (errorMessage === "already_uploaded_self") {
                    refinedError = "You have already uploaded an unpublished copy of '" + file.name + "' below."
                }
                else if (errorMessage === "already_published_self") {
                    refinedError = "You have already published a copy of '" + file.name + "' to memories."
                }
                else if (errorMessage === "format_not_supported") {
                    refinedError = "The file type of '" + file.name + "' is unsupported."
                }
                else if (errorMessage === "invalid_file") {
                    refinedError = "The file '" + file.name + "' is invalid."
                }
                else {
                    refinedError = "A file upload error occurred for '" + file.name + "'."
                }
                var msgEl = $(file.previewElement).find('.dz-error-message');
                msgEl.text(refinedError);

                setAlertWindow("warning", refinedError, "#error-window");
            });
        }
    };

    initUploadForm();
});


function initUploadForm() {
    // set up autocomplete fields
    performRequest(hostname + "/data?fetch=tags,people", "GET", "", function (result) {
        var tokenfieldSets = [["tags", "#tags-input", false], ["people", "#people-input", false]];
        var parsedData = JSON.parse(result);

        initMetaDataFields(parsedData, tokenfieldSets, null);
    });

    // set initial states
    setButtonProcessing($(".btn-primary, .btn-danger"), false);

    // for each panel, destroy old events and set up new events
    $(".upload-result-panel").each(function() {
        var panel = $(this);
        var fileName = panel.find(".img-details input[type=hidden]").val();

        panel.find("form").on("submit", function(e) {
            e.preventDefault();
            return false;
        });

        // perform publish file request
        panel.find("form .btn-primary").on("click", function(e) {
            e.preventDefault();
            setButtonProcessing($(this), true);

            // perform request
            performRequest(hostname + "/upload/publish", "POST", $(".upload-result-container form").serialize(), function (result) {
                result = result.trim();

                setButtonProcessing(panel.find("form .btn-primary"), false);

                if (result === "success") {
                    panel.fadeOut(500, function () {
                        panel.remove();
                    });
                    setAlertWindow("success", "File successfully published!", "#error-window");
                }
                else if (result === "no_tags") {
                    setAlertWindow("warning", "Please specify at least one tag before publishing.", "#error-window");
                }
                else if (result === "no_people") {
                    setAlertWindow("warning", "Please specify at least one person before publishing.", "#error-window");
                }
                else if (result === "already_stored") {
                    panel.fadeOut(500, function () {
                        panel.remove();
                    });
                    setAlertWindow("warning", "A copy of this file has already been stored!", "#error-window");
                }
                else {
                    setAlertWindow("danger", "A server error occurred (" + fileName + ").", "#error-window");
                }
            });
        });

        // delete image from user's temp dir
        panel.find("form .btn-danger").on("click", function(e) {
            e.preventDefault();
            setButtonProcessing($(this), true);

            // perform request
            performRequest(hostname + "/upload/temp_delete", "POST", $(".upload-result-container form").serialize(), function (result) {
                result = result.trim();

                setButtonProcessing(panel.find("form .btn-danger"), false);

                if (result === "success") {
                    panel.fadeOut(500, function() {
                        panel.remove();
                    });
                    setAlertWindow("success", "File has been deleted!", "#error-window");
                }
                else if (result === "invalid_file") {
                    panel.fadeOut(500, function() {
                        panel.remove();
                    });
                    setAlertWindow("success", "File has already been deleted!", "#error-window");
                }
                else {
                    setAlertWindow("danger", "A server error occurred (" + fileName + ").", "#error-window");
                }
            });
        });
    });
}